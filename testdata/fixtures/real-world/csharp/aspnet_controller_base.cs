// Source: https://github.com/dotnet/aspnetcore/blob/main/src/Mvc/Mvc.Core/src/ControllerBase.cs | License: MIT
// Licensed to the .NET Foundation under one or more agreements.
// The .NET Foundation licenses this file to you under the MIT license.

using System.Diagnostics;
using System.Diagnostics.CodeAnalysis;
using System.Linq.Expressions;
using System.Security.Claims;
using System.Text;
using Microsoft.AspNetCore.Authentication;
using Microsoft.AspNetCore.Http;
using Microsoft.AspNetCore.Mvc.Infrastructure;
using Microsoft.AspNetCore.Mvc.ModelBinding;
using Microsoft.AspNetCore.Mvc.ModelBinding.Validation;
using Microsoft.AspNetCore.Mvc.Routing;
using Microsoft.AspNetCore.Routing;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Net.Http.Headers;

namespace Microsoft.AspNetCore.Mvc;

/// <summary>
/// A base class for an MVC controller without view support.
/// </summary>
[Controller]
public abstract class ControllerBase
{
    private ControllerContext? _controllerContext;
    private IModelMetadataProvider? _metadataProvider;
    private IModelBinderFactory? _modelBinderFactory;
    private IObjectModelValidator? _objectValidator;
    private IUrlHelper? _url;
    private ProblemDetailsFactory? _problemDetailsFactory;

    /// <summary>
    /// Gets the <see cref="Http.HttpContext"/> for the executing action.
    /// </summary>
    public HttpContext HttpContext => ControllerContext.HttpContext;

    /// <summary>
    /// Gets the <see cref="HttpRequest"/> for the executing action.
    /// </summary>
    public HttpRequest Request => HttpContext?.Request!;

    /// <summary>
    /// Gets the <see cref="HttpResponse"/> for the executing action.
    /// </summary>
    public HttpResponse Response => HttpContext?.Response!;

    /// <summary>
    /// Gets the <see cref="AspNetCore.Routing.RouteData"/> for the executing action.
    /// </summary>
    public RouteData RouteData => ControllerContext.RouteData;

    /// <summary>
    /// Gets the <see cref="ModelStateDictionary"/> that contains the state of the model and of model-binding validation.
    /// </summary>
    public ModelStateDictionary ModelState => ControllerContext.ModelState;

    /// <summary>
    /// Gets or sets the <see cref="Mvc.ControllerContext"/>.
    /// </summary>
    /// <remarks>
    /// <see cref="Controllers.IControllerActivator"/> activates this property while activating controllers.
    /// If user code directly instantiates a controller, the getter returns an empty
    /// <see cref="Mvc.ControllerContext"/>.
    /// </remarks>
    [ControllerContext]
    public ControllerContext ControllerContext
    {
        get
        {
            if (_controllerContext == null)
            {
                _controllerContext = new ControllerContext();
            }

            return _controllerContext;
        }
        set
        {
            ArgumentNullException.ThrowIfNull(value);

            _controllerContext = value;
        }
    }

    /// <summary>
    /// Gets or sets the <see cref="IModelMetadataProvider"/>.
    /// </summary>
    [DebuggerBrowsable(DebuggerBrowsableState.Never)]
    public IModelMetadataProvider MetadataProvider
    {
        get
        {
            if (_metadataProvider == null)
            {
                _metadataProvider = HttpContext?.RequestServices?.GetRequiredService<IModelMetadataProvider>();
            }

            return _metadataProvider!;
        }
        set
        {
            ArgumentNullException.ThrowIfNull(value);

            _metadataProvider = value;
        }
    }

    /// <summary>
    /// Gets or sets the <see cref="IModelBinderFactory"/>.
    /// </summary>
    [DebuggerBrowsable(DebuggerBrowsableState.Never)]
    public IModelBinderFactory ModelBinderFactory
    {
        get
        {
            if (_modelBinderFactory == null)
            {
                _modelBinderFactory = HttpContext?.RequestServices?.GetRequiredService<IModelBinderFactory>();
            }

            return _modelBinderFactory!;
        }
        set
        {
            ArgumentNullException.ThrowIfNull(value);

            _modelBinderFactory = value;
        }
    }

    /// <summary>
    /// Gets or sets the <see cref="IUrlHelper"/>.
    /// </summary>
    [DebuggerBrowsable(DebuggerBrowsableState.Never)]
    public IUrlHelper Url
    {
        get
        {
            if (_url == null)
            {
                var factory = HttpContext?.RequestServices?.GetRequiredService<IUrlHelperFactory>();
                _url = factory?.GetUrlHelper(ControllerContext);
            }

            return _url!;
        }
        set
        {
            ArgumentNullException.ThrowIfNull(value);

            _url = value;
        }
    }

    /// <summary>
    /// Gets or sets the <see cref="IObjectModelValidator"/>.
    /// </summary>
    [DebuggerBrowsable(DebuggerBrowsableState.Never)]
    public IObjectModelValidator ObjectValidator
    {
        get
        {
            if (_objectValidator == null)
            {
                _objectValidator = HttpContext?.RequestServices?.GetRequiredService<IObjectModelValidator>();
            }

            return _objectValidator!;
        }
        set
        {
            ArgumentNullException.ThrowIfNull(value);

            _objectValidator = value;
        }
    }

    /// <summary>
    /// Gets or sets the <see cref="ProblemDetailsFactory"/>.
    /// </summary>
    [DebuggerBrowsable(DebuggerBrowsableState.Never)]
    public ProblemDetailsFactory ProblemDetailsFactory
    {
        get
        {
            if (_problemDetailsFactory == null)
            {
                _problemDetailsFactory = HttpContext?.RequestServices?.GetRequiredService<ProblemDetailsFactory>();
            }

            return _problemDetailsFactory!;
        }
        set
        {
            ArgumentNullException.ThrowIfNull(value);

            _problemDetailsFactory = value;
        }
    }

    /// <summary>
    /// Gets the <see cref="ClaimsPrincipal"/> for user associated with the executing action.
    /// </summary>
    public ClaimsPrincipal User => HttpContext?.User!;

    /// <summary>
    /// Gets an instance of <see cref="EmptyResult"/>.
    /// </summary>
    public static EmptyResult Empty { get; } = new();

    /// <summary>
    /// Creates a <see cref="StatusCodeResult"/> object by specifying a <paramref name="statusCode"/>.
    /// </summary>
    /// <param name="statusCode">The status code to set on the response.</param>
    /// <returns>The created <see cref="StatusCodeResult"/> object for the response.</returns>
    [NonAction]
    public virtual StatusCodeResult StatusCode([ActionResultStatusCode] int statusCode)
        => new StatusCodeResult(statusCode);

    /// <summary>
    /// Creates an <see cref="ObjectResult"/> object by specifying a <paramref name="statusCode"/> and <paramref name="value"/>
    /// </summary>
    /// <param name="statusCode">The status code to set on the response.</param>
    /// <param name="value">The value to set on the <see cref="ObjectResult"/>.</param>
    /// <returns>The created <see cref="ObjectResult"/> object for the response.</returns>
    [NonAction]
    public virtual ObjectResult StatusCode([ActionResultStatusCode] int statusCode, [ActionResultObjectValue] object? value)
    {
        return new ObjectResult(value)
        {
            StatusCode = statusCode
        };
    }

    /// <summary>
    /// Creates a <see cref="ContentResult"/> object by specifying a <paramref name="content"/> string.
    /// </summary>
    /// <param name="content">The content to write to the response.</param>
    /// <returns>The created <see cref="ContentResult"/> object for the response.</returns>
    [NonAction]
    public virtual ContentResult Content(string content)
        => Content(content, (MediaTypeHeaderValue?)null);

    /// <summary>
    /// Creates a <see cref="ContentResult"/> object by specifying a
    /// <paramref name="content"/> string and a content type.
    /// </summary>
    /// <param name="content">The content to write to the response.</param>
    /// <param name="contentType">The content type (MIME type).</param>
    /// <returns>The created <see cref="ContentResult"/> object for the response.</returns>
    [NonAction]
    public virtual ContentResult Content(string content, string contentType)
        => Content(content, MediaTypeHeaderValue.Parse(contentType));

    /// <summary>
    /// Creates a <see cref="ContentResult"/> object by specifying a
    /// <paramref name="content"/> string, a <paramref name="contentType"/>, and <paramref name="contentEncoding"/>.
    /// </summary>
    /// <param name="content">The content to write to the response.</param>
    /// <param name="contentType">The content type (MIME type).</param>
    /// <param name="contentEncoding">The content encoding.</param>
    /// <returns>The created <see cref="ContentResult"/> object for the response.</returns>
    /// <remarks>
    /// If encoding is provided by both the 'charset' and the <paramref name="contentEncoding"/> parameters, then
    /// the <paramref name="contentEncoding"/> parameter is chosen as the final encoding.
    /// </remarks>
    [NonAction]
    public virtual ContentResult Content(string content, string contentType, Encoding contentEncoding)
    {
        var mediaTypeHeaderValue = MediaTypeHeaderValue.Parse(contentType);
        mediaTypeHeaderValue.Encoding = contentEncoding ?? mediaTypeHeaderValue.Encoding;
        return Content(content, mediaTypeHeaderValue);
    }

    /// <summary>
    /// Creates a <see cref="ContentResult"/> object by specifying a
    /// <paramref name="content"/> string and a <paramref name="contentType"/>.
    /// </summary>
    /// <param name="content">The content to write to the response.</param>
    /// <param name="contentType">The content type (MIME type).</param>
    /// <returns>The created <see cref="ContentResult"/> object for the response.</returns>
    [NonAction]
    public virtual ContentResult Content(string content, MediaTypeHeaderValue? contentType)
    {
        return new ContentResult
        {
            Content = content,
            ContentType = contentType?.ToString()
        };
    }

    /// <summary>
    /// Creates a <see cref="NoContentResult"/> object that produces an empty
    /// <see cref="StatusCodes.Status204NoContent"/> response.
    /// </summary>
    /// <returns>The created <see cref="NoContentResult"/> object for the response.</returns>
    [NonAction]
    public virtual NoContentResult NoContent()
        => new NoContentResult();

    /// <summary>
    /// Creates an <see cref="OkResult"/> object that produces an empty <see cref="StatusCodes.Status200OK"/> response.
    /// </summary>
    /// <returns>The created <see cref="OkResult"/> for the response.</returns>
    [NonAction]
    public virtual OkResult Ok()
        => new OkResult();

    /// <summary>
    /// Creates an <see cref="OkObjectResult"/> object that produces a <see cref="StatusCodes.Status200OK"/> response.
    /// </summary>
    /// <param name="value">The content value to format in the entity body.</param>
    /// <returns>The created <see cref="OkObjectResult"/> for the response.</returns>
    [NonAction]
    public virtual OkObjectResult Ok([ActionResultObjectValue] object? value)
        => new OkObjectResult(value);

    #region RedirectResult variants
    /// <summary>
    /// Creates a <see cref="RedirectResult"/> object that redirects (<see cref="StatusCodes.Status302Found"/>)
    /// to the specified <paramref name="url"/>.
    /// </summary>
    /// <param name="url">The URL to redirect to.</param>
    /// <returns>The created <see cref="RedirectResult"/> for the response.</returns>
    [NonAction]
    public virtual RedirectResult Redirect([StringSyntax(StringSyntaxAttribute.Uri)] string url)
    {
        ArgumentException.ThrowIfNullOrEmpty(url);

        return new RedirectResult(url);
    }

    /// <summary>
    /// Creates a <see cref="RedirectResult"/> object with <see cref="RedirectResult.Permanent"/> set to true
    /// (<see cref="StatusCodes.Status301MovedPermanently"/>) using the specified <paramref name="url"/>.
    /// </summary>
    /// <param name="url">The URL to redirect to.</param>
    /// <returns>The created <see cref="RedirectResult"/> for the response.</returns>
    [NonAction]
    public virtual RedirectResult RedirectPermanent([StringSyntax(StringSyntaxAttribute.Uri)] string url)
    {
        ArgumentException.ThrowIfNullOrEmpty(url);

        return new RedirectResult(url, permanent: true);
    }

    /// <summary>
    /// Creates a <see cref="RedirectResult"/> object with <see cref="RedirectResult.Permanent"/> set to false
    /// and <see cref="RedirectResult.PreserveMethod"/> set to true (<see cref="StatusCodes.Status307TemporaryRedirect"/>)
    /// using the specified <paramref name="url"/>.
    /// </summary>
    /// <param name="url">The URL to redirect to.</param>
    /// <returns>The created <see cref="RedirectResult"/> for the response.</returns>
    [NonAction]
    public virtual RedirectResult RedirectPreserveMethod([StringSyntax(StringSyntaxAttribute.Uri)] string url)
    {
        ArgumentException.ThrowIfNullOrEmpty(url);

        return new RedirectResult(url: url, permanent: false, preserveMethod: true);
    }

    /// <summary>
    /// Creates a <see cref="RedirectResult"/> object with <see cref="RedirectResult.Permanent"/> set to true
    /// and <see cref="RedirectResult.PreserveMethod"/> set to true (<see cref="StatusCodes.Status308PermanentRedirect"/>)
    /// using the specified <paramref name="url"/>.
    /// </summary>
    /// <param name="url">The URL to redirect to.</param>
    /// <returns>The created <see cref="RedirectResult"/> for the response.</returns>
    [NonAction]
    public virtual RedirectResult RedirectPermanentPreserveMethod([StringSyntax(StringSyntaxAttribute.Uri)] string url)
    {
        ArgumentException.ThrowIfNullOrEmpty(url);

        return new RedirectResult(url: url, permanent: true, preserveMethod: true);
    }

    /// <summary>
    /// Creates a <see cref="LocalRedirectResult"/> object that redirects
    /// (<see cref="StatusCodes.Status302Found"/>) to the specified local <paramref name="localUrl"/>.
    /// </summary>
    /// <param name="localUrl">The local URL to redirect to.</param>
    /// <returns>The created <see cref="LocalRedirectResult"/> for the response.</returns>
    [NonAction]
    public virtual LocalRedirectResult LocalRedirect([StringSyntax(StringSyntaxAttribute.Uri, UriKind.Relative)] string localUrl)
    {
        ArgumentException.ThrowIfNullOrEmpty(localUrl);

        return new LocalRedirectResult(localUrl);
    }

    /// <summary>
    /// Creates a <see cref="LocalRedirectResult"/> object with <see cref="LocalRedirectResult.Permanent"/> set to
    /// true (<see cref="StatusCodes.Status301MovedPermanently"/>) using the specified <paramref name="localUrl"/>.
    /// </summary>
    /// <param name="localUrl">The local URL to redirect to.</param>
    /// <returns>The created <see cref="LocalRedirectResult"/> for the response.</returns>
    [NonAction]
    public virtual LocalRedirectResult LocalRedirectPermanent([StringSyntax(StringSyntaxAttribute.Uri, UriKind.Relative)] string localUrl)
