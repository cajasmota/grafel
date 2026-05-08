// Source: https://github.com/akka/akka-http/blob/main/akka-http-core/src/main/scala/akka/http/impl/engine/server/HttpServerBluePrint.scala | License: Apache-2.0
/*
 * Copyright (C) 2009-2025 Lightbend Inc. <https://akka.io>
 */

package akka.http.impl.engine.server

import java.util.concurrent.atomic.AtomicReference
import scala.concurrent.{ Future, Promise }
import scala.concurrent.duration.{ Deadline, Duration, FiniteDuration }
import scala.collection.immutable
import scala.util.control.{ NoStackTrace, NonFatal }
import akka.NotUsed
import akka.actor.Cancellable
import akka.annotation.InternalApi
import akka.japi.Function
import akka.event.LoggingAdapter
import akka.http.ParsingErrorHandler
import akka.util.ByteString
import akka.stream._
import akka.stream.TLSProtocol._
import akka.stream.scaladsl._
import akka.stream.stage._
import akka.http.scaladsl.settings.ServerSettings
import akka.http.impl.engine.parsing.ParserOutput._
import akka.http.impl.engine.parsing._
import akka.http.impl.engine.rendering.ResponseRenderingContext.CloseRequested
import akka.http.impl.engine.rendering.{ DateHeaderRendering, HttpResponseRendererFactory, ResponseRenderingContext, ResponseRenderingOutput }
import akka.http.impl.util._
import akka.http.scaladsl.util.FastFuture.EnhancedFuture
import akka.http.scaladsl.{ Http, TimeoutAccess }
import akka.http.scaladsl.model.headers.`Timeout-Access`
import akka.http.javadsl.model
import akka.http.scaladsl.model._
import akka.http.impl.util.LogByteStringTools._

import scala.annotation.nowarn
import scala.concurrent.ExecutionContext
import scala.util.Failure

/**
 * INTERNAL API
 *
 *
 * HTTP pipeline setup (without the underlying SSL/TLS (un)wrapping and the websocket switch):
 *
 *                 +----------+          +-------------+          +-------------+             +-----------+
 *    HttpRequest  |          |   Http-  |  request-   | Request- |             |   Request-  | request-  | ByteString
 *  | <------------+          <----------+ Preparation <----------+             <-------------+  Parsing  <-----------
 *  |              |          |  Request |             | Output   |             |   Output    |           |
 *  |              |          |          +-------------+          |             |             +-----------+
 *  |              |          |                                   |             |
 *  | Application- | One2One- |                                   | controller- |
 *  | Flow         |   Bidi   |                                   |    Stage    |
 *  |              |          |                                   |             |
 *  |              |          |                                   |             |             +-----------+
 *  | HttpResponse |          |           HttpResponse            |             |  Response-  | renderer- | ByteString
 *  v ------------->          +----------------------------------->             +-------------> Pipeline  +---------->
 *                 |          |                                   |             |  Rendering- |           |
 *                 +----------+                                   +-------------+  Context    +-----------+
 */
@InternalApi
private[http] object HttpServerBluePrint {
  def apply(settings: ServerSettings, log: LoggingAdapter, isSecureConnection: Boolean, dateHeaderRendering: DateHeaderRendering): Http.ServerLayer =
    userHandlerGuard(settings.pipeliningLimit) atop
      requestTimeoutSupport(settings.timeouts.requestTimeout, log) atop
      requestPreparation(settings) atop
      controller(settings, log) atop
      parsingRendering(settings, log, isSecureConnection, dateHeaderRendering) atop
      websocketSupport(settings, log) atop
      tlsSupport atop
      logTLSBidiBySetting("server-plain-text", settings.logUnencryptedNetworkBytes)

  val tlsSupport: BidiFlow[ByteString, SslTlsOutbound, SslTlsInbound, SessionBytes, NotUsed] =
    BidiFlow.fromFlows(Flow[ByteString].map(SendBytes(_)), Flow[SslTlsInbound].collect { case x: SessionBytes => x })

  def websocketSupport(settings: ServerSettings, log: LoggingAdapter): BidiFlow[ResponseRenderingOutput, ByteString, SessionBytes, SessionBytes, NotUsed] =
    BidiFlow.fromGraph(new ProtocolSwitchStage(settings, log))

  def parsingRendering(settings: ServerSettings, log: LoggingAdapter, isSecureConnection: Boolean, dateHeaderRendering: DateHeaderRendering): BidiFlow[ResponseRenderingContext, ResponseRenderingOutput, SessionBytes, RequestOutput, NotUsed] =
    BidiFlow.fromFlows(rendering(settings, log, dateHeaderRendering), parsing(settings, log, isSecureConnection))

  def controller(settings: ServerSettings, log: LoggingAdapter): BidiFlow[HttpResponse, ResponseRenderingContext, RequestOutput, RequestOutput, NotUsed] =
    BidiFlow.fromGraph(new ControllerStage(settings, log)).reversed

  def requestPreparation(settings: ServerSettings): BidiFlow[HttpResponse, HttpResponse, RequestOutput, HttpRequest, NotUsed] =
    BidiFlow.fromFlows(Flow[HttpResponse], new PrepareRequests(settings))

  def requestTimeoutSupport(timeout: Duration, log: LoggingAdapter): BidiFlow[HttpResponse, HttpResponse, HttpRequest, HttpRequest, NotUsed] =
    if (timeout == Duration.Zero) BidiFlow.identity[HttpResponse, HttpRequest]
    else BidiFlow.fromGraph(new RequestTimeoutSupport(timeout, log)).reversed

  /**
   * Two state stage, either transforms an incoming RequestOutput into a HttpRequest with strict entity and then pushes
   * that (the "idle" inHandler) or creates a HttpRequest with a streamed entity and switch to a state which will push
   * incoming chunks into the streaming entity until end of request is reached (the StreamedEntityCreator case in create
   * entity).
   */
  final class PrepareRequests(settings: ServerSettings) extends GraphStage[FlowShape[RequestOutput, HttpRequest]] {
    val in = Inlet[RequestOutput]("PrepareRequests.in")
    val out = Outlet[HttpRequest]("PrepareRequests.out")
    override val shape: FlowShape[RequestOutput, HttpRequest] = FlowShape.of(in, out)

    override def createLogic(inheritedAttributes: Attributes) = new GraphStageLogic(shape) with InHandler with OutHandler {
      val remoteAddressOpt = inheritedAttributes.get[HttpAttributes.RemoteAddress].map(_.address)

      var downstreamPullWaiting = false
      var completionDeferred = false
      var entitySource: SubSourceOutlet[RequestOutput] = _

      // optimization: to avoid allocations the "idle" case in and out handlers are put directly on the GraphStageLogic itself
      override def onPull(): Unit = {
        pull(in)
      }

      // optimization: this callback is used to handle entity substream cancellation to avoid allocating a dedicated handler
      override def onDownstreamFinish(cause: Throwable): Unit = {
        if (entitySource ne null) {
          // application layer has cancelled or only partially consumed response entity:
          // connection will be closed
          entitySource.complete()
        }
        completeStage()
      }

      override def onUpstreamFinish(): Unit = super.onUpstreamFinish()
      override def onUpstreamFailure(ex: Throwable): Unit = {
        if (entitySource ne null) {
          // application layer has cancelled or only partially consumed response entity:
          // connection will be closed
          entitySource.fail(ex)
        }
        super.onUpstreamFailure(ex)
      }

      override def onPush(): Unit = grab(in) match {
        case RequestStart(method, uri, protocol, attrs, hdrs, entityCreator, _, _) =>
          val effectiveMethod = if (method == HttpMethods.HEAD && settings.transparentHeadRequests) HttpMethods.GET else method

          @nowarn("msg=use remote-address-attribute instead")
          val effectiveHeaders =
            if (settings.remoteAddressHeader && remoteAddressOpt.isDefined)
              headers.`Remote-Address`(RemoteAddress(remoteAddressOpt.get)) +: hdrs
            else hdrs

          val entity = createEntity(entityCreator) withSizeLimit settings.parserSettings.maxContentLength
          val httpRequest = HttpRequest(effectiveMethod, uri, effectiveHeaders, entity, protocol)
            .withAttributes(attrs)

          val effectiveHttpRequest = if (settings.remoteAddressAttribute) {
            remoteAddressOpt.fold(httpRequest) { remoteAddress =>
              httpRequest.addAttribute(AttributeKeys.remoteAddress, RemoteAddress(remoteAddress))
            }
          } else httpRequest

          push(out, effectiveHttpRequest)
        case other =>
          throw new IllegalStateException(s"unexpected element of type ${other.getClass}")
      }

      setIdleHandlers()

      def setIdleHandlers(): Unit = {
        if (completionDeferred) {
          completeStage()
        } else {
          setHandler(in, this)
          setHandler(out, this)
          if (downstreamPullWaiting) {
            downstreamPullWaiting = false
            pull(in)
          }
        }
      }

      def createEntity(creator: EntityCreator[RequestOutput, RequestEntity]): RequestEntity =
        creator match {
          case StrictEntityCreator(entity)    => entity
          case StreamedEntityCreator(creator) => streamRequestEntity(creator)
        }

      def streamRequestEntity(creator: (Source[ParserOutput.RequestOutput, NotUsed]) => RequestEntity): RequestEntity = {
        // stream incoming chunks into the request entity until we reach the end of it
        // and then toggle back to "idle"

        entitySource = new SubSourceOutlet[RequestOutput]("EntitySource")
        // optimization: re-use the idle outHandler
        entitySource.setHandler(this)

        // optimization: handlers are combined to reduce allocations
        val chunkedRequestHandler = new InHandler with OutHandler {
          def onPush(): Unit = {
            grab(in) match {
              case MessageEnd =>
                entitySource.complete()
                entitySource = null
                setIdleHandlers()

              case x => entitySource.push(x)
            }
          }
          override def onUpstreamFinish(): Unit = {
            entitySource.complete()
            completeStage()
          }
          override def onUpstreamFailure(ex: Throwable): Unit = {
            entitySource.fail(ex)
            failStage(ex)
          }
          override def onPull(): Unit = {
            // remember this until we are done with the chunked entity
            // so can pull downstream then
            downstreamPullWaiting = true
          }
          override def onDownstreamFinish(cause: Throwable): Unit = {
            // downstream signalled not wanting any more requests
            // we should keep processing the entity stream and then
            // when it completes complete the stage
            completionDeferred = true
          }
        }

        setHandler(in, chunkedRequestHandler)
        setHandler(out, chunkedRequestHandler)
        creator(Source.fromGraph(entitySource.source))
      }

    }
  }

  def parsing(settings: ServerSettings, log: LoggingAdapter, isSecureConnection: Boolean): Flow[SessionBytes, RequestOutput, NotUsed] = {
    import settings._

    // the initial header parser we initially use for every connection,
    // will not be mutated, all "shared copy" parsers copy on first-write into the header cache
    val rootParser = new HttpRequestParser(parserSettings, websocketSettings, rawRequestUriHeader, HttpHeaderParser(parserSettings, log))

    def establishAbsoluteUri(requestOutput: RequestOutput): RequestOutput = requestOutput match {
      case connect: RequestStart if connect.method == HttpMethods.CONNECT =>
        MessageStartError(StatusCodes.BadRequest, ErrorInfo(s"CONNECT requests are not supported", s"Rejecting CONNECT request to '${connect.uri}'"))
      case start: RequestStart =>
        try {
          val effectiveUri = HttpRequest.effectiveUri(start.uri, start.headers, isSecureConnection, defaultHostHeader)
          start.copy(uri = effectiveUri)
        } catch {
          case e: IllegalUriException =>
            MessageStartError(StatusCodes.BadRequest, e.info)
        }
      case x => x
    }

    Flow[SessionBytes].via(rootParser).map(establishAbsoluteUri)
  }

  def rendering(settings: ServerSettings, log: LoggingAdapter, dateHeaderRendering: DateHeaderRendering): Flow[ResponseRenderingContext, ResponseRenderingOutput, NotUsed] = {
    import settings._

    val responseRendererFactory = new HttpResponseRendererFactory(serverHeader, responseHeaderSizeHint, log, dateHeaderRendering)

    Flow[ResponseRenderingContext]
      .via(responseRendererFactory.renderer.named("renderer"))
  }

  class RequestTimeoutSupport(initialTimeout: Duration, log: LoggingAdapter)
    extends GraphStage[BidiShape[HttpRequest, HttpRequest, HttpResponse, HttpResponse]] {
    private val requestIn = Inlet[HttpRequest]("RequestTimeoutSupport.requestIn")
    private val requestOut = Outlet[HttpRequest]("RequestTimeoutSupport.requestOut")
    private val responseIn = Inlet[HttpResponse]("RequestTimeoutSupport.responseIn")
    private val responseOut = Outlet[HttpResponse]("RequestTimeoutSupport.responseOut")

    override def initialAttributes = Attributes.name("RequestTimeoutSupport")

    val shape = new BidiShape(requestIn, requestOut, responseIn, responseOut)

    def createLogic(effectiveAttributes: Attributes) = new GraphStageLogic(shape) {
      var openTimeouts = immutable.Queue[TimeoutAccessImpl]()
      // the application response might has already arrived after we scheduled the timeout response (which is close but ok)
      // or current head (same reason) is not for response the timeout has been scheduled for
      val callback: AsyncCallback[(TimeoutAccess, HttpResponse)] = getAsyncCallback {
        case (timeout, response) =>
          if (openTimeouts.headOption.exists(_ eq timeout)) {
            emit(responseOut, response, () => completeStage())
          }
      }
      setHandler(requestIn, new InHandler {
        def onPush(): Unit = {
          val request = grab(requestIn)
          val (entity, requestEnd) = HttpEntity.captureTermination(request.entity)
          val access = new TimeoutAccessImpl(request, initialTimeout, requestEnd, callback,
            interpreter.materializer, log)
          openTimeouts = openTimeouts.enqueue(access)
          push(requestOut, request.addHeader(`Timeout-Access`(access)).withEntity(entity))
        }
        override def onUpstreamFinish() = complete(requestOut)
        override def onUpstreamFailure(ex: Throwable) = fail(requestOut, ex)
      })
      // TODO: provide and use default impl for simply connecting an input and an output port as we do here
      setHandler(requestOut, new OutHandler {
        def onPull(): Unit = pull(requestIn)
        override def onDownstreamFinish(cause: Throwable) = cancel(requestIn)
      })
      setHandler(responseIn, new InHandler {
        def onPush(): Unit = {
          openTimeouts.head.clear()
          openTimeouts = openTimeouts.tail
          push(responseOut, grab(responseIn))
        }
        override def onUpstreamFinish() = complete(responseOut)
        override def onUpstreamFailure(ex: Throwable) = fail(responseOut, ex)
      })
      setHandler(responseOut, new OutHandler {
        def onPull(): Unit = pull(responseIn)
        override def onDownstreamFinish(cause: Throwable) = cancel(responseIn)
      })
    }
  }

  private class TimeoutSetup(
    val timeoutBase:   Deadline,
    val scheduledTask: Cancellable,
    val timeout:       Duration,
    val handler:       HttpRequest => HttpResponse)

  private object DummyCancellable extends Cancellable {
    override def isCancelled: Boolean = true
    override def cancel(): Boolean = true
  }

  private class TimeoutAccessImpl(request: HttpRequest, initialTimeout: Duration, requestEnd: Future[Unit],
                                  trigger:      AsyncCallback[(TimeoutAccess, HttpResponse)],
                                  materializer: Materializer, log: LoggingAdapter)
    extends AtomicReference[Future[TimeoutSetup]] with TimeoutAccess with (HttpRequest => HttpResponse) { self =>
    import materializer.executionContext

    private var currentTimeout = initialTimeout

    initialTimeout match {
      case timeout: FiniteDuration => set {
        requestEnd.fast.map(_ => new TimeoutSetup(Deadline.now, schedule(timeout, this), timeout, this))
      }
      case _ => set {
        requestEnd.fast.map(_ => new TimeoutSetup(Deadline.now, DummyCancellable, Duration.Inf, this))
      }
    }

    override def apply(request: HttpRequest) = {
      log.info("Request timeout encountered for request [{}]", request.debugString)
      //#default-request-timeout-httpresponse
      HttpResponse(StatusCodes.ServiceUnavailable, entity = "The server was not able " +
        "to produce a timely response to your request.\r\nPlease try again in a short while!")
      //#default-request-timeout-httpresponse
