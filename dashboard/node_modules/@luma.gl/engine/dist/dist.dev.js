(function webpackUniversalModuleDefinition(root, factory) {
  if (typeof exports === 'object' && typeof module === 'object')
    module.exports = factory();
  else if (typeof define === 'function' && define.amd) define([], factory);
        else if (typeof exports === 'object') exports['luma'] = factory();
  else root['luma'] = factory();})(globalThis, function () {
"use strict";
var __exports__ = (() => {
  var __create = Object.create;
  var __defProp = Object.defineProperty;
  var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
  var __getOwnPropNames = Object.getOwnPropertyNames;
  var __getProtoOf = Object.getPrototypeOf;
  var __hasOwnProp = Object.prototype.hasOwnProperty;
  var __defNormalProp = (obj, key, value) => key in obj ? __defProp(obj, key, { enumerable: true, configurable: true, writable: true, value }) : obj[key] = value;
  var __commonJS = (cb, mod) => function __require() {
    return mod || (0, cb[__getOwnPropNames(cb)[0]])((mod = { exports: {} }).exports, mod), mod.exports;
  };
  var __export = (target, all) => {
    for (var name in all)
      __defProp(target, name, { get: all[name], enumerable: true });
  };
  var __copyProps = (to, from, except, desc) => {
    if (from && typeof from === "object" || typeof from === "function") {
      for (let key of __getOwnPropNames(from))
        if (!__hasOwnProp.call(to, key) && key !== except)
          __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });
    }
    return to;
  };
  var __reExport = (target, mod, secondTarget) => (__copyProps(target, mod, "default"), secondTarget && __copyProps(secondTarget, mod, "default"));
  var __toESM = (mod, isNodeMode, target) => (target = mod != null ? __create(__getProtoOf(mod)) : {}, __copyProps(
    // If the importer is in node compatibility mode or this is not an ESM
    // file that has been converted to a CommonJS file using a Babel-
    // compatible transform (i.e. "__esModule" has not been set), then set
    // "default" to the CommonJS "module.exports" for node compatibility.
    isNodeMode || !mod || !mod.__esModule ? __defProp(target, "default", { value: mod, enumerable: true }) : target,
    mod
  ));
  var __toCommonJS = (mod) => __copyProps(__defProp({}, "__esModule", { value: true }), mod);
  var __publicField = (obj, key, value) => {
    __defNormalProp(obj, typeof key !== "symbol" ? key + "" : key, value);
    return value;
  };

  // external-global-plugin:@luma.gl/core
  var require_core = __commonJS({
    "external-global-plugin:@luma.gl/core"(exports, module) {
      module.exports = globalThis.luma;
    }
  });

  // external-global-plugin:@luma.gl/shadertools
  var require_shadertools = __commonJS({
    "external-global-plugin:@luma.gl/shadertools"(exports, module) {
      module.exports = globalThis.luma;
    }
  });

  // bundle.ts
  var bundle_exports = {};
  __export(bundle_exports, {
    AnimationLoop: () => AnimationLoop,
    AnimationLoopTemplate: () => AnimationLoopTemplate,
    AsyncTexture: () => AsyncTexture,
    BackgroundTextureModel: () => BackgroundTextureModel,
    BufferTransform: () => BufferTransform,
    ClipSpace: () => ClipSpace,
    Computation: () => Computation,
    ConeGeometry: () => ConeGeometry,
    CubeGeometry: () => CubeGeometry,
    CylinderGeometry: () => CylinderGeometry,
    GPUGeometry: () => GPUGeometry,
    Geometry: () => Geometry,
    GroupNode: () => GroupNode,
    IcoSphereGeometry: () => IcoSphereGeometry,
    KeyFrames: () => KeyFrames,
    LegacyPickingManager: () => LegacyPickingManager,
    Model: () => Model,
    ModelNode: () => ModelNode,
    PickingManager: () => PickingManager,
    PipelineFactory: () => PipelineFactory,
    PlaneGeometry: () => PlaneGeometry,
    ScenegraphNode: () => ScenegraphNode,
    ShaderFactory: () => ShaderFactory,
    ShaderInputs: () => ShaderInputs,
    ShaderPassRenderer: () => ShaderPassRenderer,
    SphereGeometry: () => SphereGeometry,
    Swap: () => Swap,
    SwapBuffers: () => SwapBuffers,
    SwapFramebuffers: () => SwapFramebuffers,
    TextureTransform: () => TextureTransform,
    Timeline: () => Timeline,
    TruncatedConeGeometry: () => TruncatedConeGeometry,
    cancelAnimationFramePolyfill: () => cancelAnimationFramePolyfill,
    colorPicking: () => picking2,
    indexPicking: () => picking,
    loadImage: () => loadImage,
    loadImageBitmap: () => loadImageBitmap,
    makeAnimationLoop: () => makeAnimationLoop,
    makeRandomGenerator: () => makeRandomGenerator,
    requestAnimationFramePolyfill: () => requestAnimationFramePolyfill,
    setPathPrefix: () => setPathPrefix
  });
  __reExport(bundle_exports, __toESM(require_core(), 1));

  // src/animation/timeline.ts
  var channelHandles = 1;
  var animationHandles = 1;
  var Timeline = class {
    time = 0;
    channels = /* @__PURE__ */ new Map();
    animations = /* @__PURE__ */ new Map();
    playing = false;
    lastEngineTime = -1;
    constructor() {
    }
    addChannel(props) {
      const { delay = 0, duration = Number.POSITIVE_INFINITY, rate = 1, repeat = 1 } = props;
      const channelId = channelHandles++;
      const channel = {
        time: 0,
        delay,
        duration,
        rate,
        repeat
      };
      this._setChannelTime(channel, this.time);
      this.channels.set(channelId, channel);
      return channelId;
    }
    removeChannel(channelId) {
      this.channels.delete(channelId);
      for (const [animationHandle, animation] of this.animations) {
        if (animation.channel === channelId) {
          this.detachAnimation(animationHandle);
        }
      }
    }
    isFinished(channelId) {
      const channel = this.channels.get(channelId);
      if (channel === void 0) {
        return false;
      }
      return this.time >= channel.delay + channel.duration * channel.repeat;
    }
    getTime(channelId) {
      if (channelId === void 0) {
        return this.time;
      }
      const channel = this.channels.get(channelId);
      if (channel === void 0) {
        return -1;
      }
      return channel.time;
    }
    setTime(time) {
      this.time = Math.max(0, time);
      const channels = this.channels.values();
      for (const channel of channels) {
        this._setChannelTime(channel, this.time);
      }
      const animations = this.animations.values();
      for (const animationData of animations) {
        const { animation, channel } = animationData;
        animation.setTime(this.getTime(channel));
      }
    }
    play() {
      this.playing = true;
    }
    pause() {
      this.playing = false;
      this.lastEngineTime = -1;
    }
    reset() {
      this.setTime(0);
    }
    attachAnimation(animation, channelHandle) {
      const animationHandle = animationHandles++;
      this.animations.set(animationHandle, {
        animation,
        channel: channelHandle
      });
      animation.setTime(this.getTime(channelHandle));
      return animationHandle;
    }
    detachAnimation(channelId) {
      this.animations.delete(channelId);
    }
    update(engineTime) {
      if (this.playing) {
        if (this.lastEngineTime === -1) {
          this.lastEngineTime = engineTime;
        }
        this.setTime(this.time + (engineTime - this.lastEngineTime));
        this.lastEngineTime = engineTime;
      }
    }
    _setChannelTime(channel, time) {
      const offsetTime = time - channel.delay;
      const totalDuration = channel.duration * channel.repeat;
      if (offsetTime >= totalDuration) {
        channel.time = channel.duration * channel.rate;
      } else {
        channel.time = Math.max(0, offsetTime) % channel.duration;
        channel.time *= channel.rate;
      }
    }
  };

  // src/animation/key-frames.ts
  var KeyFrames = class {
    startIndex = -1;
    endIndex = -1;
    factor = 0;
    times = [];
    values = [];
    _lastTime = -1;
    constructor(keyFrames) {
      this.setKeyFrames(keyFrames);
      this.setTime(0);
    }
    setKeyFrames(keyFrames) {
      const numKeys = keyFrames.length;
      this.times.length = numKeys;
      this.values.length = numKeys;
      for (let i = 0; i < numKeys; ++i) {
        this.times[i] = keyFrames[i][0];
        this.values[i] = keyFrames[i][1];
      }
      this._calculateKeys(this._lastTime);
    }
    setTime(time) {
      time = Math.max(0, time);
      if (time !== this._lastTime) {
        this._calculateKeys(time);
        this._lastTime = time;
      }
    }
    getStartTime() {
      return this.times[this.startIndex];
    }
    getEndTime() {
      return this.times[this.endIndex];
    }
    getStartData() {
      return this.values[this.startIndex];
    }
    getEndData() {
      return this.values[this.endIndex];
    }
    _calculateKeys(time) {
      let index = 0;
      const numKeys = this.times.length;
      for (index = 0; index < numKeys - 2; ++index) {
        if (this.times[index + 1] > time) {
          break;
        }
      }
      this.startIndex = index;
      this.endIndex = index + 1;
      const startTime = this.times[this.startIndex];
      const endTime = this.times[this.endIndex];
      this.factor = Math.min(Math.max(0, (time - startTime) / (endTime - startTime)), 1);
    }
  };

  // src/animation-loop/animation-loop-template.ts
  var AnimationLoopTemplate = class {
    constructor(animationProps) {
    }
    async onInitialize(animationProps) {
      return null;
    }
  };

  // src/animation-loop/animation-loop.ts
  var import_core = __toESM(require_core(), 1);

  // src/animation-loop/request-animation-frame.ts
  function requestAnimationFramePolyfill(callback) {
    return typeof window !== "undefined" && window.requestAnimationFrame ? window.requestAnimationFrame(callback) : setTimeout(callback, 1e3 / 60);
  }
  function cancelAnimationFramePolyfill(timerId) {
    return typeof window !== "undefined" && window.cancelAnimationFrame ? window.cancelAnimationFrame(timerId) : clearTimeout(timerId);
  }

  // ../../node_modules/@probe.gl/stats/dist/utils/hi-res-timestamp.js
  function getHiResTimestamp() {
    let timestamp;
    if (typeof window !== "undefined" && window.performance) {
      timestamp = window.performance.now();
    } else if (typeof process !== "undefined" && process.hrtime) {
      const timeParts = process.hrtime();
      timestamp = timeParts[0] * 1e3 + timeParts[1] / 1e6;
    } else {
      timestamp = Date.now();
    }
    return timestamp;
  }

  // ../../node_modules/@probe.gl/stats/dist/lib/stat.js
  var Stat = class {
    constructor(name, type) {
      this.sampleSize = 1;
      this.time = 0;
      this.count = 0;
      this.samples = 0;
      this.lastTiming = 0;
      this.lastSampleTime = 0;
      this.lastSampleCount = 0;
      this._count = 0;
      this._time = 0;
      this._samples = 0;
      this._startTime = 0;
      this._timerPending = false;
      this.name = name;
      this.type = type;
      this.reset();
    }
    reset() {
      this.time = 0;
      this.count = 0;
      this.samples = 0;
      this.lastTiming = 0;
      this.lastSampleTime = 0;
      this.lastSampleCount = 0;
      this._count = 0;
      this._time = 0;
      this._samples = 0;
      this._startTime = 0;
      this._timerPending = false;
      return this;
    }
    setSampleSize(samples) {
      this.sampleSize = samples;
      return this;
    }
    /** Call to increment count (+1) */
    incrementCount() {
      this.addCount(1);
      return this;
    }
    /** Call to decrement count (-1) */
    decrementCount() {
      this.subtractCount(1);
      return this;
    }
    /** Increase count */
    addCount(value) {
      this._count += value;
      this._samples++;
      this._checkSampling();
      return this;
    }
    /** Decrease count */
    subtractCount(value) {
      this._count -= value;
      this._samples++;
      this._checkSampling();
      return this;
    }
    /** Add an arbitrary timing and bump the count */
    addTime(time) {
      this._time += time;
      this.lastTiming = time;
      this._samples++;
      this._checkSampling();
      return this;
    }
    /** Start a timer */
    timeStart() {
      this._startTime = getHiResTimestamp();
      this._timerPending = true;
      return this;
    }
    /** End a timer. Adds to time and bumps the timing count. */
    timeEnd() {
      if (!this._timerPending) {
        return this;
      }
      this.addTime(getHiResTimestamp() - this._startTime);
      this._timerPending = false;
      this._checkSampling();
      return this;
    }
    getSampleAverageCount() {
      return this.sampleSize > 0 ? this.lastSampleCount / this.sampleSize : 0;
    }
    /** Calculate average time / count for the previous window */
    getSampleAverageTime() {
      return this.sampleSize > 0 ? this.lastSampleTime / this.sampleSize : 0;
    }
    /** Calculate counts per second for the previous window */
    getSampleHz() {
      return this.lastSampleTime > 0 ? this.sampleSize / (this.lastSampleTime / 1e3) : 0;
    }
    getAverageCount() {
      return this.samples > 0 ? this.count / this.samples : 0;
    }
    /** Calculate average time / count */
    getAverageTime() {
      return this.samples > 0 ? this.time / this.samples : 0;
    }
    /** Calculate counts per second */
    getHz() {
      return this.time > 0 ? this.samples / (this.time / 1e3) : 0;
    }
    _checkSampling() {
      if (this._samples === this.sampleSize) {
        this.lastSampleTime = this._time;
        this.lastSampleCount = this._count;
        this.count += this._count;
        this.time += this._time;
        this.samples += this._samples;
        this._time = 0;
        this._count = 0;
        this._samples = 0;
      }
    }
  };

  // ../../node_modules/@probe.gl/stats/dist/lib/stats.js
  var Stats = class {
    constructor(options) {
      this.stats = {};
      this.id = options.id;
      this.stats = {};
      this._initializeStats(options.stats);
      Object.seal(this);
    }
    /** Acquire a stat. Create if it doesn't exist. */
    get(name, type = "count") {
      return this._getOrCreate({ name, type });
    }
    get size() {
      return Object.keys(this.stats).length;
    }
    /** Reset all stats */
    reset() {
      for (const stat of Object.values(this.stats)) {
        stat.reset();
      }
      return this;
    }
    forEach(fn) {
      for (const stat of Object.values(this.stats)) {
        fn(stat);
      }
    }
    getTable() {
      const table = {};
      this.forEach((stat) => {
        table[stat.name] = {
          time: stat.time || 0,
          count: stat.count || 0,
          average: stat.getAverageTime() || 0,
          hz: stat.getHz() || 0
        };
      });
      return table;
    }
    _initializeStats(stats = []) {
      stats.forEach((stat) => this._getOrCreate(stat));
    }
    _getOrCreate(stat) {
      const { name, type } = stat;
      let result = this.stats[name];
      if (!result) {
        if (stat instanceof Stat) {
          result = stat;
        } else {
          result = new Stat(name, type);
        }
        this.stats[name] = result;
      }
      return result;
    }
  };

  // src/animation-loop/animation-loop.ts
  var statIdCounter = 0;
  var _AnimationLoop = class {
    device = null;
    canvas = null;
    props;
    animationProps = null;
    timeline = null;
    stats;
    cpuTime;
    gpuTime;
    frameRate;
    display;
    needsRedraw = "initialized";
    _initialized = false;
    _running = false;
    _animationFrameId = null;
    _nextFramePromise = null;
    _resolveNextFrame = null;
    _cpuStartTime = 0;
    _error = null;
    // _gpuTimeQuery: Query | null = null;
    /*
     * @param {HTMLCanvasElement} canvas - if provided, width and height will be passed to context
     */
    constructor(props) {
      this.props = { ..._AnimationLoop.defaultAnimationLoopProps, ...props };
      props = this.props;
      if (!props.device) {
        throw new Error("No device provided");
      }
      this.stats = props.stats || new Stats({ id: "animation-loop-stats" });
      this.cpuTime = this.stats.get("CPU Time");
      this.gpuTime = this.stats.get("GPU Time");
      this.frameRate = this.stats.get("Frame Rate");
      this.setProps({ autoResizeViewport: props.autoResizeViewport });
      this.start = this.start.bind(this);
      this.stop = this.stop.bind(this);
      this._onMousemove = this._onMousemove.bind(this);
      this._onMouseleave = this._onMouseleave.bind(this);
    }
    destroy() {
      this.stop();
      this._setDisplay(null);
    }
    /** @deprecated Use .destroy() */
    delete() {
      this.destroy();
    }
    reportError(error) {
      this.props.onError(error);
      this._error = error;
    }
    /** Flags this animation loop as needing redraw */
    setNeedsRedraw(reason) {
      this.needsRedraw = this.needsRedraw || reason;
      return this;
    }
    setProps(props) {
      if ("autoResizeViewport" in props) {
        this.props.autoResizeViewport = props.autoResizeViewport || false;
      }
      return this;
    }
    /** Starts a render loop if not already running */
    async start() {
      if (this._running) {
        return this;
      }
      this._running = true;
      try {
        let appContext;
        if (!this._initialized) {
          this._initialized = true;
          await this._initDevice();
          this._initialize();
          await this.props.onInitialize(this._getAnimationProps());
        }
        if (!this._running) {
          return null;
        }
        if (appContext !== false) {
          this._cancelAnimationFrame();
          this._requestAnimationFrame();
        }
        return this;
      } catch (err) {
        const error = err instanceof Error ? err : new Error("Unknown error");
        this.props.onError(error);
        throw error;
      }
    }
    /** Stops a render loop if already running, finalizing */
    stop() {
      if (this._running) {
        if (this.animationProps && !this._error) {
          this.props.onFinalize(this.animationProps);
        }
        this._cancelAnimationFrame();
        this._nextFramePromise = null;
        this._resolveNextFrame = null;
        this._running = false;
      }
      return this;
    }
    /** Explicitly draw a frame */
    redraw() {
      if (this.device?.isLost || this._error) {
        return this;
      }
      this._beginFrameTimers();
      this._setupFrame();
      this._updateAnimationProps();
      this._renderFrame(this._getAnimationProps());
      this._clearNeedsRedraw();
      if (this._resolveNextFrame) {
        this._resolveNextFrame(this);
        this._nextFramePromise = null;
        this._resolveNextFrame = null;
      }
      this._endFrameTimers();
      return this;
    }
    /** Add a timeline, it will be automatically updated by the animation loop. */
    attachTimeline(timeline) {
      this.timeline = timeline;
      return this.timeline;
    }
    /** Remove a timeline */
    detachTimeline() {
      this.timeline = null;
    }
    /** Wait until a render completes */
    waitForRender() {
      this.setNeedsRedraw("waitForRender");
      if (!this._nextFramePromise) {
        this._nextFramePromise = new Promise((resolve) => {
          this._resolveNextFrame = resolve;
        });
      }
      return this._nextFramePromise;
    }
    /** TODO - should use device.deviceContext */
    async toDataURL() {
      this.setNeedsRedraw("toDataURL");
      await this.waitForRender();
      if (this.canvas instanceof HTMLCanvasElement) {
        return this.canvas.toDataURL();
      }
      throw new Error("OffscreenCanvas");
    }
    // PRIVATE METHODS
    _initialize() {
      this._startEventHandling();
      this._initializeAnimationProps();
      this._updateAnimationProps();
      this._resizeViewport();
    }
    _setDisplay(display) {
      if (this.display) {
        this.display.destroy();
        this.display.animationLoop = null;
      }
      if (display) {
        display.animationLoop = this;
      }
      this.display = display;
    }
    _requestAnimationFrame() {
      if (!this._running) {
        return;
      }
      this._animationFrameId = requestAnimationFramePolyfill(this._animationFrame.bind(this));
    }
    _cancelAnimationFrame() {
      if (this._animationFrameId === null) {
        return;
      }
      cancelAnimationFramePolyfill(this._animationFrameId);
      this._animationFrameId = null;
    }
    _animationFrame() {
      if (!this._running) {
        return;
      }
      this.redraw();
      this._requestAnimationFrame();
    }
    // Called on each frame, can be overridden to call onRender multiple times
    // to support e.g. stereoscopic rendering
    _renderFrame(animationProps) {
      if (this.display) {
        this.display._renderFrame(animationProps);
        return;
      }
      this.props.onRender(this._getAnimationProps());
      this.device?.submit();
    }
    _clearNeedsRedraw() {
      this.needsRedraw = false;
    }
    _setupFrame() {
      this._resizeViewport();
    }
    // Initialize the  object that will be passed to app callbacks
    _initializeAnimationProps() {
      const canvasContext = this.device?.getDefaultCanvasContext();
      if (!this.device || !canvasContext) {
        throw new Error("loop");
      }
      const canvas2 = canvasContext?.canvas;
      const useDevicePixels = canvasContext.props.useDevicePixels;
      this.animationProps = {
        animationLoop: this,
        device: this.device,
        canvasContext,
        canvas: canvas2,
        // @ts-expect-error Deprecated
        useDevicePixels,
        timeline: this.timeline,
        needsRedraw: false,
        // Placeholders
        width: 1,
        height: 1,
        aspect: 1,
        // Animation props
        time: 0,
        startTime: Date.now(),
        engineTime: 0,
        tick: 0,
        tock: 0,
        // Experimental
        _mousePosition: null
        // Event props
      };
    }
    _getAnimationProps() {
      if (!this.animationProps) {
        throw new Error("animationProps");
      }
      return this.animationProps;
    }
    // Update the context object that will be passed to app callbacks
    _updateAnimationProps() {
      if (!this.animationProps) {
        return;
      }
      const { width, height, aspect } = this._getSizeAndAspect();
      if (width !== this.animationProps.width || height !== this.animationProps.height) {
        this.setNeedsRedraw("drawing buffer resized");
      }
      if (aspect !== this.animationProps.aspect) {
        this.setNeedsRedraw("drawing buffer aspect changed");
      }
      this.animationProps.width = width;
      this.animationProps.height = height;
      this.animationProps.aspect = aspect;
      this.animationProps.needsRedraw = this.needsRedraw;
      this.animationProps.engineTime = Date.now() - this.animationProps.startTime;
      if (this.timeline) {
        this.timeline.update(this.animationProps.engineTime);
      }
      this.animationProps.tick = Math.floor(this.animationProps.time / 1e3 * 60);
      this.animationProps.tock++;
      this.animationProps.time = this.timeline ? this.timeline.getTime() : this.animationProps.engineTime;
    }
    /** Wait for supplied device */
    async _initDevice() {
      this.device = await this.props.device;
      if (!this.device) {
        throw new Error("No device provided");
      }
      this.canvas = this.device.getDefaultCanvasContext().canvas || null;
    }
    _createInfoDiv() {
      if (this.canvas && this.props.onAddHTML) {
        const wrapperDiv = document.createElement("div");
        document.body.appendChild(wrapperDiv);
        wrapperDiv.style.position = "relative";
        const div = document.createElement("div");
        div.style.position = "absolute";
        div.style.left = "10px";
        div.style.bottom = "10px";
        div.style.width = "300px";
        div.style.background = "white";
        if (this.canvas instanceof HTMLCanvasElement) {
          wrapperDiv.appendChild(this.canvas);
        }
        wrapperDiv.appendChild(div);
        const html = this.props.onAddHTML(div);
        if (html) {
          div.innerHTML = html;
        }
      }
    }
    _getSizeAndAspect() {
      if (!this.device) {
        return { width: 1, height: 1, aspect: 1 };
      }
      const [width, height] = this.device?.getDefaultCanvasContext().getDevicePixelSize() || [1, 1];
      let aspect = 1;
      const canvas2 = this.device?.getDefaultCanvasContext().canvas;
      if (canvas2 && canvas2.clientHeight) {
        aspect = canvas2.clientWidth / canvas2.clientHeight;
      } else if (width > 0 && height > 0) {
        aspect = width / height;
      }
      return { width, height, aspect };
    }
    /** @deprecated Default viewport setup */
    _resizeViewport() {
      if (this.props.autoResizeViewport && this.device.gl) {
        this.device.gl.viewport(
          0,
          0,
          // @ts-expect-error Expose canvasContext
          this.device.gl.drawingBufferWidth,
          // @ts-expect-error Expose canvasContext
          this.device.gl.drawingBufferHeight
        );
      }
    }
    _beginFrameTimers() {
      this.frameRate.timeEnd();
      this.frameRate.timeStart();
      this.cpuTime.timeStart();
    }
    _endFrameTimers() {
      this.cpuTime.timeEnd();
    }
    // Event handling
    _startEventHandling() {
      if (this.canvas) {
        this.canvas.addEventListener("mousemove", this._onMousemove.bind(this));
        this.canvas.addEventListener("mouseleave", this._onMouseleave.bind(this));
      }
    }
    _onMousemove(event) {
      if (event instanceof MouseEvent) {
        this._getAnimationProps()._mousePosition = [event.offsetX, event.offsetY];
      }
    }
    _onMouseleave(event) {
      this._getAnimationProps()._mousePosition = null;
    }
  };
  var AnimationLoop = _AnimationLoop;
  __publicField(AnimationLoop, "defaultAnimationLoopProps", {
    device: null,
    onAddHTML: () => "",
    onInitialize: async () => null,
    onRender: () => {
    },
    onFinalize: () => {
    },
    onError: (error) => console.error(error),
    // eslint-disable-line no-console
    stats: import_core.luma.stats.get(`animation-loop-${statIdCounter++}`),
    // view parameters
    autoResizeViewport: false
  });

  // src/animation-loop/make-animation-loop.ts
  var import_core2 = __toESM(require_core(), 1);
  function makeAnimationLoop(AnimationLoopTemplateCtor, props) {
    let renderLoop = null;
    const device = props?.device || import_core2.luma.createDevice({ id: "animation-loop", adapters: props?.adapters, createCanvasContext: true });
    const animationLoop = new AnimationLoop({
      ...props,
      device,
      async onInitialize(animationProps) {
        clearError(animationProps.animationLoop.device);
        try {
          renderLoop = new AnimationLoopTemplateCtor(animationProps);
          return await renderLoop?.onInitialize(animationProps);
        } catch (error) {
          setError(animationProps.animationLoop.device, error);
          return null;
        }
      },
      onRender: (animationProps) => renderLoop?.onRender(animationProps),
      onFinalize: (animationProps) => renderLoop?.onFinalize(animationProps)
    });
    animationLoop.getInfo = () => {
      return this.AnimationLoopTemplateCtor.info;
    };
    return animationLoop;
  }
  function setError(device, error) {
    const canvas2 = device?.getDefaultCanvasContext().canvas;
    if (canvas2 instanceof HTMLCanvasElement) {
      canvas2.style.overflow = "visible";
      let errorDiv = document.getElementById("animation-loop-error");
      errorDiv?.remove();
      errorDiv = document.createElement("h1");
      errorDiv.id = "animation-loop-error";
      errorDiv.innerHTML = error.message;
      errorDiv.style.position = "absolute";
      errorDiv.style.top = "10px";
      errorDiv.style.left = "10px";
      errorDiv.style.color = "black";
      errorDiv.style.backgroundColor = "red";
      canvas2.parentElement?.appendChild(errorDiv);
    }
  }
  function clearError(device) {
    const errorDiv = document.getElementById("animation-loop-error");
    if (errorDiv) {
      errorDiv.remove();
    }
  }

  // src/model/model.ts
  var import_core9 = __toESM(require_core(), 1);
  var import_shadertools2 = __toESM(require_shadertools(), 1);

  // src/geometry/gpu-geometry.ts
  var import_core3 = __toESM(require_core(), 1);

  // src/utils/uid.ts
  var uidCounters = {};
  function uid(id = "id") {
    uidCounters[id] = uidCounters[id] || 1;
    const count = uidCounters[id]++;
    return `${id}-${count}`;
  }

  // src/geometry/gpu-geometry.ts
  var GPUGeometry = class {
    id;
    userData = {};
    /** Determines how vertices are read from the 'vertex' attributes */
    topology;
    bufferLayout = [];
    vertexCount;
    indices;
    attributes;
    constructor(props) {
      this.id = props.id || uid("geometry");
      this.topology = props.topology;
      this.indices = props.indices || null;
      this.attributes = props.attributes;
      this.vertexCount = props.vertexCount;
      this.bufferLayout = props.bufferLayout || [];
      if (this.indices) {
        if (!(this.indices.usage & import_core3.Buffer.INDEX)) {
          throw new Error("Index buffer must have INDEX usage");
        }
      }
    }
    destroy() {
      this.indices?.destroy();
      for (const attribute of Object.values(this.attributes)) {
        attribute.destroy();
      }
    }
    getVertexCount() {
      return this.vertexCount;
    }
    getAttributes() {
      return this.attributes;
    }
    getIndexes() {
      return this.indices || null;
    }
    _calculateVertexCount(positions) {
      const vertexCount = positions.byteLength / 12;
      return vertexCount;
    }
  };
  function makeGPUGeometry(device, geometry) {
    if (geometry instanceof GPUGeometry) {
      return geometry;
    }
    const indices = getIndexBufferFromGeometry(device, geometry);
    const { attributes, bufferLayout } = getAttributeBuffersFromGeometry(device, geometry);
    return new GPUGeometry({
      topology: geometry.topology || "triangle-list",
      bufferLayout,
      vertexCount: geometry.vertexCount,
      indices,
      attributes
    });
  }
  function getIndexBufferFromGeometry(device, geometry) {
    if (!geometry.indices) {
      return void 0;
    }
    const data = geometry.indices.value;
    return device.createBuffer({ usage: import_core3.Buffer.INDEX, data });
  }
  function getAttributeBuffersFromGeometry(device, geometry) {
    const bufferLayout = [];
    const attributes = {};
    for (const [attributeName, attribute] of Object.entries(geometry.attributes)) {
      let name = attributeName;
      switch (attributeName) {
        case "POSITION":
          name = "positions";
          break;
        case "NORMAL":
          name = "normals";
          break;
        case "TEXCOORD_0":
          name = "texCoords";
          break;
        case "COLOR_0":
          name = "colors";
          break;
      }
      if (attribute) {
        attributes[name] = device.createBuffer({
          data: attribute.value,
          id: `${attributeName}-buffer`
        });
        const { value, size, normalized } = attribute;
        bufferLayout.push({ name, format: (0, import_core3.getVertexFormatFromAttribute)(value, size, normalized) });
      }
    }
    const vertexCount = geometry._calculateVertexCount(geometry.attributes, geometry.indices);
    return { attributes, bufferLayout, vertexCount };
  }

  // src/factories/pipeline-factory.ts
  var import_core4 = __toESM(require_core(), 1);
  var _PipelineFactory = class {
    /** Get the singleton default pipeline factory for the specified device */
    static getDefaultPipelineFactory(device) {
      device._lumaData["defaultPipelineFactory"] = device._lumaData["defaultPipelineFactory"] || new _PipelineFactory(device);
      return device._lumaData["defaultPipelineFactory"];
    }
    device;
    cachingEnabled;
    destroyPolicy;
    debug;
    _hashCounter = 0;
    _hashes = {};
    _renderPipelineCache = {};
    _computePipelineCache = {};
    get [Symbol.toStringTag]() {
      return "PipelineFactory";
    }
    toString() {
      return `PipelineFactory(${this.device.id})`;
    }
    constructor(device) {
      this.device = device;
      this.cachingEnabled = device.props._cachePipelines;
      this.destroyPolicy = device.props._cacheDestroyPolicy;
      this.debug = device.props.debugFactories;
    }
    /** Return a RenderPipeline matching supplied props. Reuses an equivalent pipeline if already created. */
    createRenderPipeline(props) {
      if (!this.cachingEnabled) {
        return this.device.createRenderPipeline(props);
      }
      const allProps = { ...import_core4.RenderPipeline.defaultProps, ...props };
      const cache = this._renderPipelineCache;
      const hash = this._hashRenderPipeline(allProps);
      let pipeline = cache[hash]?.pipeline;
      if (!pipeline) {
        pipeline = this.device.createRenderPipeline({
          ...allProps,
          id: allProps.id ? `${allProps.id}-cached` : uid("unnamed-cached")
        });
        pipeline.hash = hash;
        cache[hash] = { pipeline, useCount: 1 };
        if (this.debug) {
          import_core4.log.log(3, `${this}: ${pipeline} created, count=${cache[hash].useCount}`)();
        }
      } else {
        cache[hash].useCount++;
        if (this.debug) {
          import_core4.log.log(
            3,
            `${this}: ${cache[hash].pipeline} reused, count=${cache[hash].useCount}, (id=${props.id})`
          )();
        }
      }
      return pipeline;
    }
    /** Return a ComputePipeline matching supplied props. Reuses an equivalent pipeline if already created. */
    createComputePipeline(props) {
      if (!this.cachingEnabled) {
        return this.device.createComputePipeline(props);
      }
      const allProps = { ...import_core4.ComputePipeline.defaultProps, ...props };
      const cache = this._computePipelineCache;
      const hash = this._hashComputePipeline(allProps);
      let pipeline = cache[hash]?.pipeline;
      if (!pipeline) {
        pipeline = this.device.createComputePipeline({
          ...allProps,
          id: allProps.id ? `${allProps.id}-cached` : void 0
        });
        pipeline.hash = hash;
        cache[hash] = { pipeline, useCount: 1 };
        if (this.debug) {
          import_core4.log.log(3, `${this}: ${pipeline} created, count=${cache[hash].useCount}`)();
        }
      } else {
        cache[hash].useCount++;
        if (this.debug) {
          import_core4.log.log(
            3,
            `${this}: ${cache[hash].pipeline} reused, count=${cache[hash].useCount}, (id=${props.id})`
          )();
        }
      }
      return pipeline;
    }
    release(pipeline) {
      if (!this.cachingEnabled) {
        pipeline.destroy();
        return;
      }
      const cache = this._getCache(pipeline);
      const hash = pipeline.hash;
      cache[hash].useCount--;
      if (cache[hash].useCount === 0) {
        this._destroyPipeline(pipeline);
        if (this.debug) {
          import_core4.log.log(3, `${this}: ${pipeline} released and destroyed`)();
        }
      } else if (cache[hash].useCount < 0) {
        import_core4.log.error(`${this}: ${pipeline} released, useCount < 0, resetting`)();
        cache[hash].useCount = 0;
      } else if (this.debug) {
        import_core4.log.log(3, `${this}: ${pipeline} released, count=${cache[hash].useCount}`)();
      }
    }
    // PRIVATE
    /** Destroy a cached pipeline, removing it from the cache (depending on destroy policy) */
    _destroyPipeline(pipeline) {
      const cache = this._getCache(pipeline);
      switch (this.destroyPolicy) {
        case "never":
          return false;
        case "unused":
          delete cache[pipeline.hash];
          pipeline.destroy();
          return true;
      }
    }
    /** Get the appropriate cache for the type of pipeline */
    _getCache(pipeline) {
      let cache;
      if (pipeline instanceof import_core4.ComputePipeline) {
        cache = this._computePipelineCache;
      }
      if (pipeline instanceof import_core4.RenderPipeline) {
        cache = this._renderPipelineCache;
      }
      if (!cache) {
        throw new Error(`${this}`);
      }
      if (!cache[pipeline.hash]) {
        throw new Error(`${this}: ${pipeline} matched incorrect entry`);
      }
      return cache;
    }
    /** Calculate a hash based on all the inputs for a compute pipeline */
    _hashComputePipeline(props) {
      const { type } = this.device;
      const shaderHash = this._getHash(props.shader.source);
      return `${type}/C/${shaderHash}`;
    }
    /** Calculate a hash based on all the inputs for a render pipeline */
    _hashRenderPipeline(props) {
      const vsHash = props.vs ? this._getHash(props.vs.source) : 0;
      const fsHash = props.fs ? this._getHash(props.fs.source) : 0;
      const varyingHash = "-";
      const bufferLayoutHash = this._getHash(JSON.stringify(props.bufferLayout));
      const { type } = this.device;
      switch (type) {
        case "webgl":
          return `${type}/R/${vsHash}/${fsHash}V${varyingHash}BL${bufferLayoutHash}`;
        case "webgpu":
        default:
          const parameterHash = this._getHash(JSON.stringify(props.parameters));
          return `${type}/R/${vsHash}/${fsHash}V${varyingHash}T${props.topology}P${parameterHash}BL${bufferLayoutHash}`;
      }
    }
    _getHash(key) {
      if (this._hashes[key] === void 0) {
        this._hashes[key] = this._hashCounter++;
      }
      return this._hashes[key];
    }
  };
  var PipelineFactory = _PipelineFactory;
  __publicField(PipelineFactory, "defaultProps", { ...import_core4.RenderPipeline.defaultProps });

  // src/factories/shader-factory.ts
  var import_core5 = __toESM(require_core(), 1);
  var _ShaderFactory = class {
    /** Returns the default ShaderFactory for the given {@link Device}, creating one if necessary. */
    static getDefaultShaderFactory(device) {
      device._lumaData["defaultShaderFactory"] ||= new _ShaderFactory(device);
      return device._lumaData["defaultShaderFactory"];
    }
    device;
    cachingEnabled;
    destroyPolicy;
    debug;
    _cache = {};
    get [Symbol.toStringTag]() {
      return "ShaderFactory";
    }
    toString() {
      return `${this[Symbol.toStringTag]}(${this.device.id})`;
    }
    /** @internal */
    constructor(device) {
      this.device = device;
      this.cachingEnabled = device.props._cacheShaders;
      this.destroyPolicy = device.props._cacheDestroyPolicy;
      this.debug = true;
    }
    /** Requests a {@link Shader} from the cache, creating a new Shader only if necessary. */
    createShader(props) {
      if (!this.cachingEnabled) {
        return this.device.createShader(props);
      }
      const key = this._hashShader(props);
      let cacheEntry = this._cache[key];
      if (!cacheEntry) {
        const shader = this.device.createShader({
          ...props,
          id: props.id ? `${props.id}-cached` : void 0
        });
        this._cache[key] = cacheEntry = { shader, useCount: 1 };
        if (this.debug) {
          import_core5.log.log(3, `${this}: Created new shader ${shader.id}`)();
        }
      } else {
        cacheEntry.useCount++;
        if (this.debug) {
          import_core5.log.log(
            3,
            `${this}: Reusing shader ${cacheEntry.shader.id} count=${cacheEntry.useCount}`
          )();
        }
      }
      return cacheEntry.shader;
    }
    /** Releases a previously-requested {@link Shader}, destroying it if no users remain. */
    release(shader) {
      if (!this.cachingEnabled) {
        shader.destroy();
        return;
      }
      const key = this._hashShader(shader);
      const cacheEntry = this._cache[key];
      if (cacheEntry) {
        cacheEntry.useCount--;
        if (cacheEntry.useCount === 0) {
          if (this.destroyPolicy === "unused") {
            delete this._cache[key];
            cacheEntry.shader.destroy();
            if (this.debug) {
              import_core5.log.log(3, `${this}: Releasing shader ${shader.id}, destroyed`)();
            }
          }
        } else if (cacheEntry.useCount < 0) {
          throw new Error(`ShaderFactory: Shader ${shader.id} released too many times`);
        } else if (this.debug) {
          import_core5.log.log(3, `${this}: Releasing shader ${shader.id} count=${cacheEntry.useCount}`)();
        }
      }
    }
    // PRIVATE
    _hashShader(value) {
      return `${value.stage}:${value.source}`;
    }
  };
  var ShaderFactory = _ShaderFactory;
  __publicField(ShaderFactory, "defaultProps", { ...import_core5.Shader.defaultProps });

  // src/debug/debug-shader-layout.ts
  function getDebugTableForShaderLayout(layout, name) {
    const table = {};
    const header = "Values";
    if (layout.attributes.length === 0 && !layout.varyings?.length) {
      return { "No attributes or varyings": { [header]: "N/A" } };
    }
    for (const attributeDeclaration of layout.attributes) {
      if (attributeDeclaration) {
        const glslDeclaration = `${attributeDeclaration.location} ${attributeDeclaration.name}: ${attributeDeclaration.type}`;
        table[`in ${glslDeclaration}`] = { [header]: attributeDeclaration.stepMode || "vertex" };
      }
    }
    for (const varyingDeclaration of layout.varyings || []) {
      const glslDeclaration = `${varyingDeclaration.location} ${varyingDeclaration.name}`;
      table[`out ${glslDeclaration}`] = { [header]: JSON.stringify(varyingDeclaration) };
    }
    return table;
  }

  // src/debug/debug-framebuffer.ts
  var canvas = null;
  var ctx = null;
  function debugFramebuffer(fbo, {
    id,
    minimap,
    opaque,
    top = "0",
    left = "0",
    rgbaScale = 1
  }) {
    if (!canvas) {
      canvas = document.createElement("canvas");
      canvas.id = id;
      canvas.title = id;
      canvas.style.zIndex = "100";
      canvas.style.position = "absolute";
      canvas.style.top = top;
      canvas.style.left = left;
      canvas.style.border = "blue 5px solid";
      canvas.style.transform = "scaleY(-1)";
      document.body.appendChild(canvas);
      ctx = canvas.getContext("2d");
    }
    if (canvas.width !== fbo.width || canvas.height !== fbo.height) {
      canvas.width = fbo.width / 2;
      canvas.height = fbo.height / 2;
      canvas.style.width = "400px";
      canvas.style.height = "400px";
    }
    const color = fbo.device.readPixelsToArrayWebGL(fbo);
    const imageData = ctx?.createImageData(fbo.width, fbo.height);
    if (imageData) {
      const offset = 0;
      for (let i = 0; i < color.length; i += 4) {
        imageData.data[offset + i + 0] = color[i + 0] * rgbaScale;
        imageData.data[offset + i + 1] = color[i + 1] * rgbaScale;
        imageData.data[offset + i + 2] = color[i + 2] * rgbaScale;
        imageData.data[offset + i + 3] = opaque ? 255 : color[i + 3] * rgbaScale;
      }
      ctx?.putImageData(imageData, 0, 0);
    }
  }

  // src/utils/deep-equal.ts
  function deepEqual(a, b, depth) {
    if (a === b) {
      return true;
    }
    if (!depth || !a || !b) {
      return false;
    }
    if (Array.isArray(a)) {
      if (!Array.isArray(b) || a.length !== b.length) {
        return false;
      }
      for (let i = 0; i < a.length; i++) {
        if (!deepEqual(a[i], b[i], depth - 1)) {
          return false;
        }
      }
      return true;
    }
    if (Array.isArray(b)) {
      return false;
    }
    if (typeof a === "object" && typeof b === "object") {
      const aKeys = Object.keys(a);
      const bKeys = Object.keys(b);
      if (aKeys.length !== bKeys.length) {
        return false;
      }
      for (const key of aKeys) {
        if (!b.hasOwnProperty(key)) {
          return false;
        }
        if (!deepEqual(a[key], b[key], depth - 1)) {
          return false;
        }
      }
      return true;
    }
    return false;
  }

  // src/utils/buffer-layout-helper.ts
  var import_core6 = __toESM(require_core(), 1);
  var BufferLayoutHelper = class {
    bufferLayouts;
    constructor(bufferLayouts) {
      this.bufferLayouts = bufferLayouts;
    }
    getBufferLayout(name) {
      return this.bufferLayouts.find((layout) => layout.name === name) || null;
    }
    /** Get attribute names from a BufferLayout */
    getAttributeNamesForBuffer(bufferLayout) {
      return bufferLayout.attributes ? bufferLayout.attributes?.map((layout) => layout.attribute) : [bufferLayout.name];
    }
    mergeBufferLayouts(bufferLayouts1, bufferLayouts2) {
      const mergedLayouts = [...bufferLayouts1];
      for (const attribute of bufferLayouts2) {
        const index = mergedLayouts.findIndex((attribute2) => attribute2.name === attribute.name);
        if (index < 0) {
          mergedLayouts.push(attribute);
        } else {
          mergedLayouts[index] = attribute;
        }
      }
      return mergedLayouts;
    }
    getBufferIndex(bufferName) {
      const bufferIndex = this.bufferLayouts.findIndex((layout) => layout.name === bufferName);
      if (bufferIndex === -1) {
        import_core6.log.warn(`BufferLayout: Missing buffer for "${bufferName}".`)();
      }
      return bufferIndex;
    }
  };

  // src/utils/buffer-layout-order.ts
  function sortedBufferLayoutByShaderSourceLocations(shaderLayout, bufferLayout) {
    const shaderLayoutMap = Object.fromEntries(
      shaderLayout.attributes.map((attr) => [attr.name, attr.location])
    );
    const sortedLayout = bufferLayout.slice();
    sortedLayout.sort((a, b) => {
      const attributeNamesA = a.attributes ? a.attributes.map((attr) => attr.attribute) : [a.name];
      const attributeNamesB = b.attributes ? b.attributes.map((attr) => attr.attribute) : [b.name];
      const minLocationA = Math.min(...attributeNamesA.map((name) => shaderLayoutMap[name]));
      const minLocationB = Math.min(...attributeNamesB.map((name) => shaderLayoutMap[name]));
      return minLocationA - minLocationB;
    });
    return sortedLayout;
  }

  // src/shader-inputs.ts
  var import_core7 = __toESM(require_core(), 1);
  var import_shadertools = __toESM(require_shadertools(), 1);

  // ../../node_modules/@math.gl/types/dist/is-array.js
  function isTypedArray(value) {
    return ArrayBuffer.isView(value) && !(value instanceof DataView);
  }
  function isNumberArray(value) {
    if (Array.isArray(value)) {
      return value.length === 0 || typeof value[0] === "number";
    }
    return false;
  }
  function isNumericArray(value) {
    return isTypedArray(value) || isNumberArray(value);
  }

  // src/model/split-uniforms-and-bindings.ts
  function isUniformValue(value) {
    return isNumericArray(value) || typeof value === "number" || typeof value === "boolean";
  }
  function splitUniformsAndBindings(uniforms) {
    const result = { bindings: {}, uniforms: {} };
    Object.keys(uniforms).forEach((name) => {
      const uniform = uniforms[name];
      if (isUniformValue(uniform)) {
        result.uniforms[name] = uniform;
      } else {
        result.bindings[name] = uniform;
      }
    });
    return result;
  }

  // src/shader-inputs.ts
  var ShaderInputs = class {
    options = {
      disableWarnings: false
    };
    /**
     * The map of modules
     * @todo should should this include the resolved dependencies?
     */
    // @ts-ignore Fix typings
    modules;
    /** Stores the uniform values for each module */
    moduleUniforms;
    /** Stores the uniform bindings for each module  */
    moduleBindings;
    /** Tracks if uniforms have changed */
    // moduleUniformsChanged: Record<keyof ShaderPropsT, false | string>;
    /**
     * Create a new UniformStore instance
     * @param modules
     */
    constructor(modules, options) {
      Object.assign(this.options, options);
      const resolvedModules = (0, import_shadertools.getShaderModuleDependencies)(
        Object.values(modules).filter((module) => module.dependencies)
      );
      for (const resolvedModule of resolvedModules) {
        modules[resolvedModule.name] = resolvedModule;
      }
      import_core7.log.log(1, "Creating ShaderInputs with modules", Object.keys(modules))();
      this.modules = modules;
      this.moduleUniforms = {};
      this.moduleBindings = {};
      for (const [name, module] of Object.entries(modules)) {
        this._addModule(module);
        if (module.name && name !== module.name && !this.options.disableWarnings) {
          import_core7.log.warn(`Module name: ${name} vs ${module.name}`)();
        }
      }
    }
    /** Destroy */
    destroy() {
    }
    /**
     * Set module props
     */
    setProps(props) {
      for (const name of Object.keys(props)) {
        const moduleName = name;
        const moduleProps = props[moduleName] || {};
        const module = this.modules[moduleName];
        if (!module) {
          if (!this.options.disableWarnings) {
            import_core7.log.warn(`Module ${name} not found`)();
          }
          continue;
        }
        const oldUniforms = this.moduleUniforms[moduleName];
        const oldBindings = this.moduleBindings[moduleName];
        const uniformsAndBindings = module.getUniforms?.(moduleProps, oldUniforms) || moduleProps;
        const { uniforms, bindings } = splitUniformsAndBindings(uniformsAndBindings);
        this.moduleUniforms[moduleName] = { ...oldUniforms, ...uniforms };
        this.moduleBindings[moduleName] = { ...oldBindings, ...bindings };
      }
    }
    /**
     * Return the map of modules
     * @todo should should this include the resolved dependencies?
     */
    getModules() {
      return Object.values(this.modules);
    }
    /** Get all uniform values for all modules */
    getUniformValues() {
      return this.moduleUniforms;
    }
    /** Merges all bindings for the shader (from the various modules) */
    getBindingValues() {
      const bindings = {};
      for (const moduleBindings of Object.values(this.moduleBindings)) {
        Object.assign(bindings, moduleBindings);
      }
      return bindings;
    }
    // INTERNAL
    /** Return a debug table that can be used for console.table() or log.table() */
    getDebugTable() {
      const table = {};
      for (const [moduleName, module] of Object.entries(this.moduleUniforms)) {
        for (const [key, value] of Object.entries(module)) {
          table[`${moduleName}.${key}`] = {
            type: this.modules[moduleName].uniformTypes?.[key],
            value: String(value)
          };
        }
      }
      return table;
    }
    _addModule(module) {
      const moduleName = module.name;
      this.moduleUniforms[moduleName] = module.defaultUniforms || {};
      this.moduleBindings[moduleName] = {};
    }
  };

  // src/async-texture/async-texture.ts
  var import_core8 = __toESM(require_core(), 1);

  // src/application-utils/load-file.ts
  var pathPrefix = "";
  function setPathPrefix(prefix) {
    pathPrefix = prefix;
  }
  async function loadImageBitmap(url, opts) {
    const image = new Image();
    image.crossOrigin = opts?.crossOrigin || "anonymous";
    image.src = url.startsWith("http") ? url : pathPrefix + url;
    await image.decode();
    return opts ? await createImageBitmap(image, opts) : await createImageBitmap(image);
  }
  async function loadImage(url, opts) {
    return await new Promise((resolve, reject) => {
      try {
        const image = new Image();
        image.onload = () => resolve(image);
        image.onerror = () => reject(new Error(`Could not load image ${url}.`));
        image.crossOrigin = opts?.crossOrigin || "anonymous";
        image.src = url.startsWith("http") ? url : pathPrefix + url;
      } catch (error) {
        reject(error);
      }
    });
  }

  // src/async-texture/async-texture.ts
  var TextureCubeFaces = ["+X", "-X", "+Y", "-Y", "+Z", "-Z"];
  var CubeFaces = ["+X", "-X", "+Y", "-Y", "+Z", "-Z"];
  var _AsyncTexture = class {
    device;
    id;
    props;
    // TODO - should we type these as possibly `null`? It will make usage harder?
    // @ts-expect-error
    texture;
    // @ts-expect-error
    sampler;
    // @ts-expect-error
    view;
    ready;
    isReady = false;
    destroyed = false;
    resolveReady = () => {
    };
    rejectReady = () => {
    };
    get [Symbol.toStringTag]() {
      return "AsyncTexture";
    }
    toString() {
      return `AsyncTexture:"${this.id}"(${this.isReady ? "ready" : "loading"})`;
    }
    constructor(device, props) {
      this.device = device;
      const id = uid("async-texture");
      this.props = { ..._AsyncTexture.defaultProps, id, ...props };
      this.id = this.props.id;
      props = { ...props };
      if (typeof props?.data === "string" && props.dimension === "2d") {
        props.data = loadImageBitmap(props.data);
      }
      if (props.mipmaps) {
        props.mipLevels = "auto";
      }
      this.ready = new Promise((resolve, reject) => {
        this.resolveReady = () => {
          this.isReady = true;
          resolve();
        };
        this.rejectReady = reject;
      });
      this.initAsync(props);
    }
    async initAsync(props) {
      const asyncData = props.data;
      const data = await awaitAllPromises(asyncData).then(
        void 0,
        this.rejectReady
      );
      if (this.destroyed) {
        return;
      }
      const size = this.props.width && this.props.height ? { width: this.props.width, height: this.props.height } : this.getTextureDataSize(data);
      if (!size) {
        throw new Error("Texture size could not be determined");
      }
      const syncProps = { ...size, ...props, data: void 0, mipLevels: 1 };
      const maxMips = this.device.getMipLevelCount(syncProps.width, syncProps.height);
      syncProps.mipLevels = this.props.mipLevels === "auto" ? maxMips : Math.min(maxMips, this.props.mipLevels);
      this.texture = this.device.createTexture(syncProps);
      this.sampler = this.texture.sampler;
      this.view = this.texture.view;
      if (props.data) {
        switch (this.props.dimension) {
          case "1d":
            this._setTexture1DData(this.texture, data);
            break;
          case "2d":
            this._setTexture2DData(data);
            break;
          case "3d":
            this._setTexture3DData(this.texture, data);
            break;
          case "2d-array":
            this._setTextureArrayData(this.texture, data);
            break;
          case "cube":
            this._setTextureCubeData(this.texture, data);
            break;
          case "cube-array":
            this._setTextureCubeArrayData(this.texture, data);
            break;
        }
      }
      if (this.props.mipmaps) {
        this.generateMipmaps();
      }
      import_core8.log.info(1, `${this} loaded`);
      this.resolveReady();
    }
    destroy() {
      if (this.texture) {
        this.texture.destroy();
        this.texture = null;
      }
      this.destroyed = true;
    }
    generateMipmaps() {
      this.texture.generateMipmapsWebGL();
    }
    /** Set sampler or create and set new Sampler from SamplerProps */
    setSampler(sampler = {}) {
      this.texture.setSampler(
        sampler instanceof import_core8.Sampler ? sampler : this.device.createSampler(sampler)
      );
    }
    /**
     * Textures are immutable and cannot be resized after creation,
     * but we can create a similar texture with the same parameters but a new size.
     * @note Does not copy contents of the texture
     * @note Mipmaps may need to be regenerated after resizing / setting new data
     * @todo Abort pending promise and create a texture with the new size?
     */
    resize(size) {
      if (!this.isReady) {
        throw new Error("Cannot resize texture before it is ready");
      }
      if (size.width === this.texture.width && size.height === this.texture.height) {
        return false;
      }
      if (this.texture) {
        const texture = this.texture;
        this.texture = texture.clone(size);
        texture.destroy();
      }
      return true;
    }
    /** Check if texture data is a typed array */
    isTextureLevelData(data) {
      const typedArray = data?.data;
      return ArrayBuffer.isView(typedArray);
    }
    /** Get the size of the texture described by the provided TextureData */
    getTextureDataSize(data) {
      if (!data) {
        return null;
      }
      if (ArrayBuffer.isView(data)) {
        return null;
      }
      if (Array.isArray(data)) {
        return this.getTextureDataSize(data[0]);
      }
      if (this.device.isExternalImage(data)) {
        return this.device.getExternalImageSize(data);
      }
      if (data && typeof data === "object" && data.constructor === Object) {
        const textureDataArray = Object.values(data);
        const untypedData = textureDataArray[0];
        return { width: untypedData.width, height: untypedData.height };
      }
      throw new Error("texture size deduction failed");
    }
    /** Convert luma.gl cubemap face constants to depth index */
    getCubeFaceDepth(face) {
      switch (face) {
        case "+X":
          return 0;
        case "-X":
          return 1;
        case "+Y":
          return 2;
        case "-Y":
          return 3;
        case "+Z":
          return 4;
        case "-Z":
          return 5;
        default:
          throw new Error(face);
      }
    }
    // EXPERIMENTAL
    setTextureData(data) {
    }
    /** Experimental: Set multiple mip levels */
    _setTexture1DData(texture, data) {
      throw new Error("setTexture1DData not supported in WebGL.");
    }
    /** Experimental: Set multiple mip levels */
    _setTexture2DData(lodData, depth = 0) {
      if (!this.texture) {
        throw new Error("Texture not initialized");
      }
      const lodArray = this._normalizeTextureData(lodData);
      if (lodArray.length > 1 && this.props.mipmaps !== false) {
        import_core8.log.warn(`Texture ${this.id} mipmap and multiple LODs.`)();
      }
      for (let mipLevel = 0; mipLevel < lodArray.length; mipLevel++) {
        const imageData = lodArray[mipLevel];
        if (this.device.isExternalImage(imageData)) {
          this.texture.copyExternalImage({ image: imageData, depth, mipLevel, flipY: true });
        } else {
          this.texture.copyImageData({ data: imageData.data, mipLevel });
        }
      }
    }
    /**
     * Experimental: Sets 3D texture data: multiple depth slices, multiple mip levels
     * @param data
     */
    _setTexture3DData(texture, data) {
      if (this.texture?.props.dimension !== "3d") {
        throw new Error(this.id);
      }
      for (let depth = 0; depth < data.length; depth++) {
        this._setTexture2DData(data[depth], depth);
      }
    }
    /**
     * Experimental: Set Cube texture data, multiple faces, multiple mip levels
     * @todo - could support TextureCubeArray with depth
     * @param data
     * @param index
     */
    _setTextureCubeData(texture, data) {
      if (this.texture?.props.dimension !== "cube") {
        throw new Error(this.id);
      }
      for (const [face, faceData] of Object.entries(data)) {
        const faceDepth = CubeFaces.indexOf(face);
        this._setTexture2DData(faceData, faceDepth);
      }
    }
    /**
     * Experimental: Sets texture array data, multiple levels, multiple depth slices
     * @param data
     */
    _setTextureArrayData(texture, data) {
      if (this.texture?.props.dimension !== "2d-array") {
        throw new Error(this.id);
      }
      for (let depth = 0; depth < data.length; depth++) {
        this._setTexture2DData(data[depth], depth);
      }
    }
    /**
     * Experimental: Sets texture cube array, multiple faces, multiple levels, multiple mip levels
     * @param data
     */
    _setTextureCubeArrayData(texture, data) {
      throw new Error("setTextureCubeArrayData not supported in WebGL2.");
    }
    /** Experimental */
    _setTextureCubeFaceData(texture, lodData, face, depth = 0) {
      if (Array.isArray(lodData) && lodData.length > 1 && this.props.mipmaps !== false) {
        import_core8.log.warn(`${this.id} has mipmap and multiple LODs.`)();
      }
      const faceDepth = TextureCubeFaces.indexOf(face);
      this._setTexture2DData(lodData, faceDepth);
    }
    /**
     * Normalize TextureData to an array of TextureImageData / ExternalImages
     * @param data
     * @param options
     * @returns array of TextureImageData / ExternalImages
     */
    _normalizeTextureData(data) {
      const options = this.texture;
      let mipLevelArray;
      if (ArrayBuffer.isView(data)) {
        mipLevelArray = [
          {
            // ts-expect-error does data really need to be Uint8ClampedArray?
            data,
            width: options.width,
            height: options.height
            // depth: options.depth
          }
        ];
      } else if (!Array.isArray(data)) {
        mipLevelArray = [data];
      } else {
        mipLevelArray = data;
      }
      return mipLevelArray;
    }
  };
  var AsyncTexture = _AsyncTexture;
  __publicField(AsyncTexture, "defaultProps", {
    ...import_core8.Texture.defaultProps,
    data: null,
    mipmaps: false
  });
  async function awaitAllPromises(x) {
    x = await x;
    if (Array.isArray(x)) {
      return await Promise.all(x.map(awaitAllPromises));
    }
    if (x && typeof x === "object" && x.constructor === Object) {
      const object = x;
      const values = await Promise.all(Object.values(object));
      const keys = Object.keys(object);
      const resolvedObject = {};
      for (let i = 0; i < keys.length; i++) {
        resolvedObject[keys[i]] = values[i];
      }
      return resolvedObject;
    }
    return x;
  }

  // src/model/model.ts
  var LOG_DRAW_PRIORITY = 2;
  var LOG_DRAW_TIMEOUT = 1e4;
  var _Model = class {
    device;
    id;
    // @ts-expect-error assigned in function called from constructor
    source;
    // @ts-expect-error assigned in function called from constructor
    vs;
    // @ts-expect-error assigned in function called from constructor
    fs;
    pipelineFactory;
    shaderFactory;
    userData = {};
    // Fixed properties (change can trigger pipeline rebuild)
    /** The render pipeline GPU parameters, depth testing etc */
    parameters;
    /** The primitive topology */
    topology;
    /** Buffer layout */
    bufferLayout;
    // Dynamic properties
    /** Use instanced rendering */
    isInstanced = void 0;
    /** instance count. `undefined` means not instanced */
    instanceCount = 0;
    /** Vertex count */
    vertexCount;
    /** Index buffer */
    indexBuffer = null;
    /** Buffer-valued attributes */
    bufferAttributes = {};
    /** Constant-valued attributes */
    constantAttributes = {};
    /** Bindings (textures, samplers, uniform buffers) */
    bindings = {};
    /**
     * VertexArray
     * @note not implemented: if bufferLayout is updated, vertex array has to be rebuilt!
     * @todo - allow application to define multiple vertex arrays?
     * */
    vertexArray;
    /** TransformFeedback, WebGL 2 only. */
    transformFeedback = null;
    /** The underlying GPU "program". @note May be recreated if parameters change */
    pipeline;
    /** ShaderInputs instance */
    // @ts-expect-error Assigned in function called by constructor
    shaderInputs;
    // @ts-expect-error Assigned in function called by constructor
    _uniformStore;
    _attributeInfos = {};
    _gpuGeometry = null;
    props;
    _pipelineNeedsUpdate = "newly created";
    _needsRedraw = "initializing";
    _destroyed = false;
    /** "Time" of last draw. Monotonically increasing timestamp */
    _lastDrawTimestamp = -1;
    get [Symbol.toStringTag]() {
      return "Model";
    }
    toString() {
      return `Model(${this.id})`;
    }
    constructor(device, props) {
      this.props = { ..._Model.defaultProps, ...props };
      props = this.props;
      this.id = props.id || uid("model");
      this.device = device;
      Object.assign(this.userData, props.userData);
      const moduleMap = Object.fromEntries(
        this.props.modules?.map((module) => [module.name, module]) || []
      );
      const shaderInputs = props.shaderInputs || new ShaderInputs(moduleMap, { disableWarnings: this.props.disableWarnings });
      this.setShaderInputs(shaderInputs);
      const platformInfo = getPlatformInfo(device);
      const modules = (
        // @ts-ignore shaderInputs is assigned in setShaderInputs above.
        (this.props.modules?.length > 0 ? this.props.modules : this.shaderInputs?.getModules()) || []
      );
      const isWebGPU = this.device.type === "webgpu";
      if (isWebGPU && this.props.source) {
        const { source: source3, getUniforms: getUniforms2 } = this.props.shaderAssembler.assembleWGSLShader({
          platformInfo,
          ...this.props,
          modules
        });
        this.source = source3;
        this._getModuleUniforms = getUniforms2;
        this.props.shaderLayout ||= (0, import_shadertools2.getShaderLayoutFromWGSL)(this.source);
      } else {
        const { vs: vs3, fs: fs3, getUniforms: getUniforms2 } = this.props.shaderAssembler.assembleGLSLShaderPair({
          platformInfo,
          ...this.props,
          modules
        });
        this.vs = vs3;
        this.fs = fs3;
        this._getModuleUniforms = getUniforms2;
      }
      this.vertexCount = this.props.vertexCount;
      this.instanceCount = this.props.instanceCount;
      this.topology = this.props.topology;
      this.bufferLayout = this.props.bufferLayout;
      this.parameters = this.props.parameters;
      if (props.geometry) {
        this.setGeometry(props.geometry);
      }
      this.pipelineFactory = props.pipelineFactory || PipelineFactory.getDefaultPipelineFactory(this.device);
      this.shaderFactory = props.shaderFactory || ShaderFactory.getDefaultShaderFactory(this.device);
      this.pipeline = this._updatePipeline();
      this.vertexArray = device.createVertexArray({
        shaderLayout: this.pipeline.shaderLayout,
        bufferLayout: this.pipeline.bufferLayout
      });
      if (this._gpuGeometry) {
        this._setGeometryAttributes(this._gpuGeometry);
      }
      if ("isInstanced" in props) {
        this.isInstanced = props.isInstanced;
      }
      if (props.instanceCount) {
        this.setInstanceCount(props.instanceCount);
      }
      if (props.vertexCount) {
        this.setVertexCount(props.vertexCount);
      }
      if (props.indexBuffer) {
        this.setIndexBuffer(props.indexBuffer);
      }
      if (props.attributes) {
        this.setAttributes(props.attributes);
      }
      if (props.constantAttributes) {
        this.setConstantAttributes(props.constantAttributes);
      }
      if (props.bindings) {
        this.setBindings(props.bindings);
      }
      if (props.transformFeedback) {
        this.transformFeedback = props.transformFeedback;
      }
      Object.seal(this);
    }
    destroy() {
      if (!this._destroyed) {
        this.pipelineFactory.release(this.pipeline);
        this.shaderFactory.release(this.pipeline.vs);
        if (this.pipeline.fs) {
          this.shaderFactory.release(this.pipeline.fs);
        }
        this._uniformStore.destroy();
        this._gpuGeometry?.destroy();
        this._destroyed = true;
      }
    }
    // Draw call
    /** Query redraw status. Clears the status. */
    needsRedraw() {
      if (this._getBindingsUpdateTimestamp() > this._lastDrawTimestamp) {
        this.setNeedsRedraw("contents of bound textures or buffers updated");
      }
      const needsRedraw = this._needsRedraw;
      this._needsRedraw = false;
      return needsRedraw;
    }
    /** Mark the model as needing a redraw */
    setNeedsRedraw(reason) {
      this._needsRedraw ||= reason;
    }
    predraw() {
      this.updateShaderInputs();
      this.pipeline = this._updatePipeline();
    }
    draw(renderPass) {
      const loadingBinding = this._areBindingsLoading();
      if (loadingBinding) {
        import_core9.log.info(LOG_DRAW_PRIORITY, `>>> DRAWING ABORTED ${this.id}: ${loadingBinding} not loaded`)();
        return false;
      }
      try {
        renderPass.pushDebugGroup(`${this}.predraw(${renderPass})`);
        this.predraw();
      } finally {
        renderPass.popDebugGroup();
      }
      let drawSuccess;
      try {
        renderPass.pushDebugGroup(`${this}.draw(${renderPass})`);
        this._logDrawCallStart();
        this.pipeline = this._updatePipeline();
        const syncBindings = this._getBindings();
        this.pipeline.setBindings(syncBindings, {
          disableWarnings: this.props.disableWarnings
        });
        const { indexBuffer } = this.vertexArray;
        const indexCount = indexBuffer ? indexBuffer.byteLength / (indexBuffer.indexType === "uint32" ? 4 : 2) : void 0;
        drawSuccess = this.pipeline.draw({
          renderPass,
          vertexArray: this.vertexArray,
          isInstanced: this.isInstanced,
          vertexCount: this.vertexCount,
          instanceCount: this.instanceCount,
          indexCount,
          transformFeedback: this.transformFeedback || void 0,
          // WebGL shares underlying cached pipelines even for models that have different parameters and topology,
          // so we must provide our unique parameters to each draw
          // (In WebGPU most parameters are encoded in the pipeline and cannot be changed per draw call)
          parameters: this.parameters,
          topology: this.topology
        });
      } finally {
        renderPass.popDebugGroup();
        this._logDrawCallEnd();
      }
      this._logFramebuffer(renderPass);
      if (drawSuccess) {
        this._lastDrawTimestamp = this.device.timestamp;
        this._needsRedraw = false;
      } else {
        this._needsRedraw = "waiting for resource initialization";
      }
      return drawSuccess;
    }
    // Update fixed fields (can trigger pipeline rebuild)
    /**
     * Updates the optional geometry
     * Geometry, set topology and bufferLayout
     * @note Can trigger a pipeline rebuild / pipeline cache fetch on WebGPU
     */
    setGeometry(geometry) {
      this._gpuGeometry?.destroy();
      const gpuGeometry = geometry && makeGPUGeometry(this.device, geometry);
      if (gpuGeometry) {
        this.setTopology(gpuGeometry.topology || "triangle-list");
        const bufferLayoutHelper = new BufferLayoutHelper(this.bufferLayout);
        this.bufferLayout = bufferLayoutHelper.mergeBufferLayouts(
          gpuGeometry.bufferLayout,
          this.bufferLayout
        );
        if (this.vertexArray) {
          this._setGeometryAttributes(gpuGeometry);
        }
      }
      this._gpuGeometry = gpuGeometry;
    }
    /**
     * Updates the primitive topology ('triangle-list', 'triangle-strip' etc).
     * @note Triggers a pipeline rebuild / pipeline cache fetch on WebGPU
     */
    setTopology(topology) {
      if (topology !== this.topology) {
        this.topology = topology;
        this._setPipelineNeedsUpdate("topology");
      }
    }
    /**
     * Updates the buffer layout.
     * @note Triggers a pipeline rebuild / pipeline cache fetch
     */
    setBufferLayout(bufferLayout) {
      const bufferLayoutHelper = new BufferLayoutHelper(this.bufferLayout);
      this.bufferLayout = this._gpuGeometry ? bufferLayoutHelper.mergeBufferLayouts(bufferLayout, this._gpuGeometry.bufferLayout) : bufferLayout;
      this._setPipelineNeedsUpdate("bufferLayout");
      this.pipeline = this._updatePipeline();
      this.vertexArray = this.device.createVertexArray({
        shaderLayout: this.pipeline.shaderLayout,
        bufferLayout: this.pipeline.bufferLayout
      });
      if (this._gpuGeometry) {
        this._setGeometryAttributes(this._gpuGeometry);
      }
    }
    /**
     * Set GPU parameters.
     * @note Can trigger a pipeline rebuild / pipeline cache fetch.
     * @param parameters
     */
    setParameters(parameters) {
      if (!deepEqual(parameters, this.parameters, 2)) {
        this.parameters = parameters;
        this._setPipelineNeedsUpdate("parameters");
      }
    }
    // Update dynamic fields
    /**
     * Updates the instance count (used in draw calls)
     * @note Any attributes with stepMode=instance need to be at least this big
     */
    setInstanceCount(instanceCount) {
      this.instanceCount = instanceCount;
      if (this.isInstanced === void 0 && instanceCount > 0) {
        this.isInstanced = true;
      }
      this.setNeedsRedraw("instanceCount");
    }
    /**
     * Updates the vertex count (used in draw calls)
     * @note Any attributes with stepMode=vertex need to be at least this big
     */
    setVertexCount(vertexCount) {
      this.vertexCount = vertexCount;
      this.setNeedsRedraw("vertexCount");
    }
    /** Set the shader inputs */
    setShaderInputs(shaderInputs) {
      this.shaderInputs = shaderInputs;
      this._uniformStore = new import_core9.UniformStore(this.shaderInputs.modules);
      for (const [moduleName, module] of Object.entries(this.shaderInputs.modules)) {
        if (shaderModuleHasUniforms(module)) {
          const uniformBuffer = this._uniformStore.getManagedUniformBuffer(this.device, moduleName);
          this.bindings[`${moduleName}Uniforms`] = uniformBuffer;
        }
      }
      this.setNeedsRedraw("shaderInputs");
    }
    /** Update uniform buffers from the model's shader inputs */
    updateShaderInputs() {
      this._uniformStore.setUniforms(this.shaderInputs.getUniformValues());
      this.setBindings(this.shaderInputs.getBindingValues());
      this.setNeedsRedraw("shaderInputs");
    }
    /**
     * Sets bindings (textures, samplers, uniform buffers)
     */
    setBindings(bindings) {
      Object.assign(this.bindings, bindings);
      this.setNeedsRedraw("bindings");
    }
    /**
     * Updates optional transform feedback. WebGL only.
     */
    setTransformFeedback(transformFeedback) {
      this.transformFeedback = transformFeedback;
      this.setNeedsRedraw("transformFeedback");
    }
    /**
     * Sets the index buffer
     * @todo - how to unset it if we change geometry?
     */
    setIndexBuffer(indexBuffer) {
      this.vertexArray.setIndexBuffer(indexBuffer);
      this.setNeedsRedraw("indexBuffer");
    }
    /**
     * Sets attributes (buffers)
     * @note Overrides any attributes previously set with the same name
     */
    setAttributes(buffers, options) {
      const disableWarnings = options?.disableWarnings ?? this.props.disableWarnings;
      if (buffers["indices"]) {
        import_core9.log.warn(
          `Model:${this.id} setAttributes() - indexBuffer should be set using setIndexBuffer()`
        )();
      }
      this.bufferLayout = sortedBufferLayoutByShaderSourceLocations(
        this.pipeline.shaderLayout,
        this.bufferLayout
      );
      const bufferLayoutHelper = new BufferLayoutHelper(this.bufferLayout);
      for (const [bufferName, buffer] of Object.entries(buffers)) {
        const bufferLayout = bufferLayoutHelper.getBufferLayout(bufferName);
        if (!bufferLayout) {
          if (!disableWarnings) {
            import_core9.log.warn(`Model(${this.id}): Missing layout for buffer "${bufferName}".`)();
          }
          continue;
        }
        const attributeNames = bufferLayoutHelper.getAttributeNamesForBuffer(bufferLayout);
        let set = false;
        for (const attributeName of attributeNames) {
          const attributeInfo = this._attributeInfos[attributeName];
          if (attributeInfo) {
            const location = this.device.type === "webgpu" ? bufferLayoutHelper.getBufferIndex(attributeInfo.bufferName) : attributeInfo.location;
            this.vertexArray.setBuffer(location, buffer);
            set = true;
          }
        }
        if (!set && !disableWarnings) {
          import_core9.log.warn(
            `Model(${this.id}): Ignoring buffer "${buffer.id}" for unknown attribute "${bufferName}"`
          )();
        }
      }
      this.setNeedsRedraw("attributes");
    }
    /**
     * Sets constant attributes
     * @note Overrides any attributes previously set with the same name
     * Constant attributes are only supported in WebGL, not in WebGPU
     * Any attribute that is disabled in the current vertex array object
     * is read from the context's global constant value for that attribute location.
     * @param constantAttributes
     */
    setConstantAttributes(attributes, options) {
      for (const [attributeName, value] of Object.entries(attributes)) {
        const attributeInfo = this._attributeInfos[attributeName];
        if (attributeInfo) {
          this.vertexArray.setConstantWebGL(attributeInfo.location, value);
        } else if (!(options?.disableWarnings ?? this.props.disableWarnings)) {
          import_core9.log.warn(
            `Model "${this.id}: Ignoring constant supplied for unknown attribute "${attributeName}"`
          )();
        }
      }
      this.setNeedsRedraw("constants");
    }
    // INTERNAL METHODS
    /** Check that bindings are loaded. Returns id of first binding that is still loading. */
    _areBindingsLoading() {
      for (const binding of Object.values(this.bindings)) {
        if (binding instanceof AsyncTexture && !binding.isReady) {
          return binding.id;
        }
      }
      return false;
    }
    /** Extracts texture view from loaded async textures. Returns null if any textures have not yet been loaded. */
    _getBindings() {
      const validBindings = {};
      for (const [name, binding] of Object.entries(this.bindings)) {
        if (binding instanceof AsyncTexture) {
          if (binding.isReady) {
            validBindings[name] = binding.texture;
          }
        } else {
          validBindings[name] = binding;
        }
      }
      return validBindings;
    }
    /** Get the timestamp of the latest updated bound GPU memory resource (buffer/texture). */
    _getBindingsUpdateTimestamp() {
      let timestamp = 0;
      for (const binding of Object.values(this.bindings)) {
        if (binding instanceof import_core9.TextureView) {
          timestamp = Math.max(timestamp, binding.texture.updateTimestamp);
        } else if (binding instanceof import_core9.Buffer || binding instanceof import_core9.Texture) {
          timestamp = Math.max(timestamp, binding.updateTimestamp);
        } else if (binding instanceof AsyncTexture) {
          timestamp = binding.texture ? Math.max(timestamp, binding.texture.updateTimestamp) : (
            // The texture will become available in the future
            Infinity
          );
        } else if (!(binding instanceof import_core9.Sampler)) {
          timestamp = Math.max(timestamp, binding.buffer.updateTimestamp);
        }
      }
      return timestamp;
    }
    /**
     * Updates the optional geometry attributes
     * Geometry, sets several attributes, indexBuffer, and also vertex count
     * @note Can trigger a pipeline rebuild / pipeline cache fetch on WebGPU
     */
    _setGeometryAttributes(gpuGeometry) {
      const attributes = { ...gpuGeometry.attributes };
      for (const [attributeName] of Object.entries(attributes)) {
        if (!this.pipeline.shaderLayout.attributes.find((layout) => layout.name === attributeName) && attributeName !== "positions") {
          delete attributes[attributeName];
        }
      }
      this.vertexCount = gpuGeometry.vertexCount;
      this.setIndexBuffer(gpuGeometry.indices || null);
      this.setAttributes(gpuGeometry.attributes, { disableWarnings: true });
      this.setAttributes(attributes, { disableWarnings: this.props.disableWarnings });
      this.setNeedsRedraw("geometry attributes");
    }
    /** Mark pipeline as needing update */
    _setPipelineNeedsUpdate(reason) {
      this._pipelineNeedsUpdate ||= reason;
      this.setNeedsRedraw(reason);
    }
    /** Update pipeline if needed */
    _updatePipeline() {
      if (this._pipelineNeedsUpdate) {
        let prevShaderVs = null;
        let prevShaderFs = null;
        if (this.pipeline) {
          import_core9.log.log(
            1,
            `Model ${this.id}: Recreating pipeline because "${this._pipelineNeedsUpdate}".`
          )();
          prevShaderVs = this.pipeline.vs;
          prevShaderFs = this.pipeline.fs;
        }
        this._pipelineNeedsUpdate = false;
        const vs3 = this.shaderFactory.createShader({
          id: `${this.id}-vertex`,
          stage: "vertex",
          source: this.source || this.vs,
          debugShaders: this.props.debugShaders
        });
        let fs3 = null;
        if (this.source) {
          fs3 = vs3;
        } else if (this.fs) {
          fs3 = this.shaderFactory.createShader({
            id: `${this.id}-fragment`,
            stage: "fragment",
            source: this.source || this.fs,
            debugShaders: this.props.debugShaders
          });
        }
        this.pipeline = this.pipelineFactory.createRenderPipeline({
          ...this.props,
          bufferLayout: this.bufferLayout,
          topology: this.topology,
          parameters: this.parameters,
          // TODO - why set bindings here when we reset them every frame?
          // Should we expose a BindGroup abstraction?
          bindings: this._getBindings(),
          vs: vs3,
          fs: fs3
        });
        this._attributeInfos = (0, import_core9.getAttributeInfosFromLayouts)(
          this.pipeline.shaderLayout,
          this.bufferLayout
        );
        if (prevShaderVs)
          this.shaderFactory.release(prevShaderVs);
        if (prevShaderFs)
          this.shaderFactory.release(prevShaderFs);
      }
      return this.pipeline;
    }
    /** Throttle draw call logging */
    _lastLogTime = 0;
    _logOpen = false;
    _logDrawCallStart() {
      const logDrawTimeout = import_core9.log.level > 3 ? 0 : LOG_DRAW_TIMEOUT;
      if (import_core9.log.level < 2 || Date.now() - this._lastLogTime < logDrawTimeout) {
        return;
      }
      this._lastLogTime = Date.now();
      this._logOpen = true;
      import_core9.log.group(LOG_DRAW_PRIORITY, `>>> DRAWING MODEL ${this.id}`, { collapsed: import_core9.log.level <= 2 })();
    }
    _logDrawCallEnd() {
      if (this._logOpen) {
        const shaderLayoutTable = getDebugTableForShaderLayout(this.pipeline.shaderLayout, this.id);
        import_core9.log.table(LOG_DRAW_PRIORITY, shaderLayoutTable)();
        const uniformTable = this.shaderInputs.getDebugTable();
        import_core9.log.table(LOG_DRAW_PRIORITY, uniformTable)();
        const attributeTable = this._getAttributeDebugTable();
        import_core9.log.table(LOG_DRAW_PRIORITY, this._attributeInfos)();
        import_core9.log.table(LOG_DRAW_PRIORITY, attributeTable)();
        import_core9.log.groupEnd(LOG_DRAW_PRIORITY)();
        this._logOpen = false;
      }
    }
    _drawCount = 0;
    _logFramebuffer(renderPass) {
      const debugFramebuffers = this.device.props.debugFramebuffers;
      this._drawCount++;
      if (!debugFramebuffers) {
        return;
      }
      const framebuffer = renderPass.props.framebuffer;
      if (framebuffer) {
        debugFramebuffer(framebuffer, { id: framebuffer.id, minimap: true });
      }
    }
    _getAttributeDebugTable() {
      const table = {};
      for (const [name, attributeInfo] of Object.entries(this._attributeInfos)) {
        const values = this.vertexArray.attributes[attributeInfo.location];
        table[attributeInfo.location] = {
          name,
          type: attributeInfo.shaderType,
          values: values ? this._getBufferOrConstantValues(values, attributeInfo.bufferDataType) : "null"
        };
      }
      if (this.vertexArray.indexBuffer) {
        const { indexBuffer } = this.vertexArray;
        const values = indexBuffer.indexType === "uint32" ? new Uint32Array(indexBuffer.debugData) : new Uint16Array(indexBuffer.debugData);
        table["indices"] = {
          name: "indices",
          type: indexBuffer.indexType,
          values: values.toString()
        };
      }
      return table;
    }
    // TODO - fix typing of luma data types
    _getBufferOrConstantValues(attribute, dataType) {
      const TypedArrayConstructor = (0, import_core9.getTypedArrayConstructor)(dataType);
      const typedArray = attribute instanceof import_core9.Buffer ? new TypedArrayConstructor(attribute.debugData) : attribute;
      return typedArray.toString();
    }
  };
  var Model = _Model;
  __publicField(Model, "defaultProps", {
    ...import_core9.RenderPipeline.defaultProps,
    source: void 0,
    vs: null,
    fs: null,
    id: "unnamed",
    handle: void 0,
    userData: {},
    defines: {},
    modules: [],
    geometry: null,
    indexBuffer: null,
    attributes: {},
    constantAttributes: {},
    varyings: [],
    isInstanced: void 0,
    instanceCount: 0,
    vertexCount: 0,
    shaderInputs: void 0,
    pipelineFactory: void 0,
    shaderFactory: void 0,
    transformFeedback: void 0,
    shaderAssembler: import_shadertools2.ShaderAssembler.getDefaultShaderAssembler(),
    debugShaders: void 0,
    disableWarnings: void 0
  });
  function shaderModuleHasUniforms(module) {
    return Boolean(module.uniformTypes && !isObjectEmpty(module.uniformTypes));
  }
  function getPlatformInfo(device) {
    return {
      type: device.type,
      shaderLanguage: device.info.shadingLanguage,
      shaderLanguageVersion: device.info.shadingLanguageVersion,
      gpu: device.info.gpu,
      // HACK - we pretend that the DeviceFeatures is a Set, it has a similar API
      features: device.features
    };
  }
  function isObjectEmpty(obj) {
    for (const key in obj) {
      return false;
    }
    return true;
  }

  // src/compute/buffer-transform.ts
  var import_core10 = __toESM(require_core(), 1);
  var import_shadertools3 = __toESM(require_shadertools(), 1);
  var _BufferTransform = class {
    device;
    model;
    transformFeedback;
    static isSupported(device) {
      return device?.info?.type === "webgl";
    }
    constructor(device, props = _BufferTransform.defaultProps) {
      if (!_BufferTransform.isSupported(device)) {
        throw new Error("BufferTransform not yet implemented on WebGPU");
      }
      this.device = device;
      this.model = new Model(this.device, {
        id: props.id || "buffer-transform-model",
        fs: props.fs || (0, import_shadertools3.getPassthroughFS)(),
        topology: props.topology || "point-list",
        varyings: props.outputs || props.varyings,
        ...props
      });
      this.transformFeedback = this.device.createTransformFeedback({
        layout: this.model.pipeline.shaderLayout,
        // @ts-expect-error TODO
        buffers: props.feedbackBuffers
      });
      this.model.setTransformFeedback(this.transformFeedback);
      Object.seal(this);
    }
    /** Destroy owned resources. */
    destroy() {
      if (this.model) {
        this.model.destroy();
      }
    }
    /** @deprecated Use {@link destroy}. */
    delete() {
      this.destroy();
    }
    /** Run one transform loop. */
    run(options) {
      if (options?.inputBuffers) {
        this.model.setAttributes(options.inputBuffers);
      }
      if (options?.outputBuffers) {
        this.transformFeedback.setBuffers(options.outputBuffers);
      }
      const renderPass = this.device.beginRenderPass(options);
      this.model.draw(renderPass);
      renderPass.end();
    }
    // DEPRECATED METHODS
    /** @deprecated App knows what buffers it is passing in - Returns the {@link Buffer} or {@link BufferRange} for given varying name. */
    getBuffer(varyingName) {
      return this.transformFeedback.getBuffer(varyingName);
    }
    /** @deprecated App knows what buffers it is passing in - Reads the {@link Buffer} or {@link BufferRange} for given varying name. */
    readAsync(varyingName) {
      const result = this.getBuffer(varyingName);
      if (!result) {
        throw new Error("BufferTransform#getBuffer");
      }
      if (result instanceof import_core10.Buffer) {
        return result.readAsync();
      }
      const { buffer, byteOffset = 0, byteLength = buffer.byteLength } = result;
      return buffer.readAsync(byteOffset, byteLength);
    }
  };
  var BufferTransform = _BufferTransform;
  __publicField(BufferTransform, "defaultProps", {
    ...Model.defaultProps,
    outputs: void 0,
    feedbackBuffers: void 0
  });

  // src/compute/texture-transform.ts
  var import_shadertools4 = __toESM(require_shadertools(), 1);
  var FS_OUTPUT_VARIABLE = "transform_output";
  var TextureTransform = class {
    device;
    model;
    sampler;
    currentIndex = 0;
    samplerTextureMap = null;
    bindings = [];
    // each element is an object : {sourceTextures, targetTexture, framebuffer}
    resources = {};
    // resources to be deleted
    constructor(device, props) {
      this.device = device;
      this.sampler = device.createSampler({
        addressModeU: "clamp-to-edge",
        addressModeV: "clamp-to-edge",
        minFilter: "nearest",
        magFilter: "nearest",
        mipmapFilter: "nearest"
      });
      this.model = new Model(this.device, {
        id: props.id || uid("texture-transform-model"),
        fs: props.fs || (0, import_shadertools4.getPassthroughFS)({
          input: props.targetTextureVarying,
          inputChannels: props.targetTextureChannels,
          output: FS_OUTPUT_VARIABLE
        }),
        vertexCount: props.vertexCount,
        // TODO(donmccurdy): Naming?
        ...props
      });
      this._initialize(props);
      Object.seal(this);
    }
    // Delete owned resources.
    destroy() {
      this.model.destroy();
      for (const binding of this.bindings) {
        binding.framebuffer?.destroy();
      }
    }
    /** @deprecated Use {@link destroy}. */
    delete() {
      this.destroy();
    }
    run(options) {
      const { framebuffer } = this.bindings[this.currentIndex];
      const renderPass = this.device.beginRenderPass({ framebuffer, ...options });
      this.model.draw(renderPass);
      renderPass.end();
      this.device.submit();
    }
    getTargetTexture() {
      const { targetTexture } = this.bindings[this.currentIndex];
      return targetTexture;
    }
    getFramebuffer() {
      const currentResources = this.bindings[this.currentIndex];
      return currentResources.framebuffer;
    }
    // Private
    _initialize(props) {
      this._updateBindings(props);
    }
    _updateBindings(props) {
      this.bindings[this.currentIndex] = this._updateBinding(this.bindings[this.currentIndex], props);
    }
    _updateBinding(binding, { sourceBuffers, sourceTextures, targetTexture }) {
      if (!binding) {
        binding = {
          sourceBuffers: {},
          sourceTextures: {},
          // @ts-expect-error
          targetTexture: null
        };
      }
      Object.assign(binding.sourceTextures, sourceTextures);
      Object.assign(binding.sourceBuffers, sourceBuffers);
      if (targetTexture) {
        binding.targetTexture = targetTexture;
        const { width, height } = targetTexture;
        if (binding.framebuffer) {
          binding.framebuffer.destroy();
        }
        binding.framebuffer = this.device.createFramebuffer({
          id: "transform-framebuffer",
          width,
          height,
          colorAttachments: [targetTexture]
        });
        binding.framebuffer.resize({ width, height });
      }
      return binding;
    }
    // set texture filtering parameters on source textures.
    _setSourceTextureParameters() {
      const index = this.currentIndex;
      const { sourceTextures } = this.bindings[index];
      for (const name in sourceTextures) {
        sourceTextures[name].sampler = this.sampler;
      }
    }
  };

  // src/geometry/geometry.ts
  var Geometry = class {
    id;
    /** Determines how vertices are read from the 'vertex' attributes */
    topology;
    vertexCount;
    indices;
    attributes;
    userData = {};
    constructor(props) {
      const { attributes = {}, indices = null, vertexCount = null } = props;
      this.id = props.id || uid("geometry");
      this.topology = props.topology;
      if (indices) {
        this.indices = ArrayBuffer.isView(indices) ? { value: indices, size: 1 } : indices;
      }
      this.attributes = {};
      for (const [attributeName, attributeValue] of Object.entries(attributes)) {
        const attribute = ArrayBuffer.isView(attributeValue) ? { value: attributeValue } : attributeValue;
        if (!ArrayBuffer.isView(attribute.value)) {
          throw new Error(
            `${this._print(attributeName)}: must be typed array or object with value as typed array`
          );
        }
        if ((attributeName === "POSITION" || attributeName === "positions") && !attribute.size) {
          attribute.size = 3;
        }
        if (attributeName === "indices") {
          if (this.indices) {
            throw new Error("Multiple indices detected");
          }
          this.indices = attribute;
        } else {
          this.attributes[attributeName] = attribute;
        }
      }
      if (this.indices && this.indices["isIndexed"] !== void 0) {
        this.indices = Object.assign({}, this.indices);
        delete this.indices["isIndexed"];
      }
      this.vertexCount = vertexCount || this._calculateVertexCount(this.attributes, this.indices);
    }
    getVertexCount() {
      return this.vertexCount;
    }
    /**
     * Return an object with all attributes plus indices added as a field.
     * TODO Geometry types are a mess
     */
    getAttributes() {
      return this.indices ? { indices: this.indices, ...this.attributes } : this.attributes;
    }
    // PRIVATE
    _print(attributeName) {
      return `Geometry ${this.id} attribute ${attributeName}`;
    }
    /**
     * GeometryAttribute
     * value: typed array
     * type: indices, vertices, uvs
     * size: elements per vertex
     * target: WebGL buffer type (string or constant)
     *
     * @param attributes
     * @param indices
     * @returns
     */
    _setAttributes(attributes, indices) {
      return this;
    }
    _calculateVertexCount(attributes, indices) {
      if (indices) {
        return indices.value.length;
      }
      let vertexCount = Infinity;
      for (const attribute of Object.values(attributes)) {
        const { value, size, constant } = attribute;
        if (!constant && value && size !== void 0 && size >= 1) {
          vertexCount = Math.min(vertexCount, value.length / size);
        }
      }
      return vertexCount;
    }
  };

  // src/models/clip-space.ts
  var CLIPSPACE_VERTEX_SHADER_WGSL = (
    /* wgsl */
    `struct VertexInputs {
  @location(0) clipSpacePosition: vec2<f32>,
  @location(1) texCoord: vec2<f32>,
  @location(2) coordinate: vec2<f32>  
}

struct FragmentInputs {
  @builtin(position) Position : vec4<f32>,
  @location(0) position : vec2<f32>,
  @location(1) coordinate : vec2<f32>,
  @location(2) uv : vec2<f32>
};

@vertex
fn vertexMain(inputs: VertexInputs) -> FragmentInputs {
  var outputs: FragmentInputs;
  outputs.Position = vec4(inputs.clipSpacePosition, 0., 1.);
  outputs.position = inputs.clipSpacePosition;
  outputs.coordinate = inputs.coordinate;
  outputs.uv = inputs.texCoord;
  return outputs;
}
`
  );
  var CLIPSPACE_VERTEX_SHADER = (
    /* glsl */
    `#version 300 es
in vec2 clipSpacePositions;
in vec2 texCoords;
in vec2 coordinates;

out vec2 position;
out vec2 coordinate;
out vec2 uv;

void main(void) {
  gl_Position = vec4(clipSpacePositions, 0., 1.);
  position = clipSpacePositions;
  coordinate = coordinates;
  uv = texCoords;
}
`
  );
  var POSITIONS = [-1, -1, 1, -1, -1, 1, 1, 1];
  var ClipSpace = class extends Model {
    constructor(device, props) {
      const TEX_COORDS = POSITIONS.map((coord) => coord === -1 ? 0 : coord);
      if (props.source) {
        props = { ...props, source: `${CLIPSPACE_VERTEX_SHADER_WGSL}
${props.source}` };
      }
      super(device, {
        id: props.id || uid("clip-space"),
        ...props,
        vs: CLIPSPACE_VERTEX_SHADER,
        vertexCount: 4,
        geometry: new Geometry({
          topology: "triangle-strip",
          vertexCount: 4,
          attributes: {
            clipSpacePositions: { size: 2, value: new Float32Array(POSITIONS) },
            texCoords: { size: 2, value: new Float32Array(TEX_COORDS) },
            coordinates: { size: 2, value: new Float32Array(TEX_COORDS) }
          }
        })
      });
    }
  };

  // src/models/billboard-texture-model.ts
  var BACKGROUND_FS_WGSL = (
    /* wgsl */
    `@group(0) @binding(0) var backgroundTexture: texture_2d<f32>;
@group(0) @binding(1) var backgroundTextureSampler: sampler;

fn billboardTexture_getTextureUV(coordinates: vec2<f32>) -> vec2<f32> {
	let iTexSize: vec2<u32> = textureDimensions(backgroundTexture, 0);
	let texSize: vec2<f32> = vec2<f32>(f32(iTexSize.x), f32(iTexSize.y));
	var position: vec2<f32> = coordinates.xy / texSize;
	return position;
} 

@fragment
fn fragmentMain(inputs: FragmentInputs) -> @location(0) vec4<f32> {
	let position: vec2<f32> = billboardTexture_getTextureUV(inputs.coordinate);
	return textureSample(backgroundTexture, backgroundTextureSampler, position);
}
`
  );
  var BACKGROUND_FS = (
    /* glsl */
    `#version 300 es
precision highp float;

uniform sampler2D backgroundTexture;
out vec4 fragColor;

vec2 billboardTexture_getTextureUV() {
  ivec2 iTexSize = textureSize(backgroundTexture, 0);
  vec2 texSize = vec2(float(iTexSize.x), float(iTexSize.y));
  vec2 position = gl_FragCoord.xy / texSize;
  return position;
}

void main(void) {
  vec2 position = billboardTexture_getTextureUV();
  fragColor = texture(backgroundTexture, position);
}
`
  );
  var BackgroundTextureModel = class extends ClipSpace {
    constructor(device, props) {
      super(device, {
        id: props.id || "background-texture-model",
        source: BACKGROUND_FS_WGSL,
        fs: BACKGROUND_FS,
        parameters: {
          depthWriteEnabled: false,
          ...props.blend ? {
            blend: true,
            blendColorOperation: "add",
            blendAlphaOperation: "add",
            blendColorSrcFactor: "one",
            blendColorDstFactor: "one-minus-src",
            blendAlphaSrcFactor: "one",
            blendAlphaDstFactor: "one-minus-src-alpha"
          } : {}
        }
      });
      if (!props.backgroundTexture) {
        throw new Error("BackgroundTextureModel requires a backgroundTexture prop");
      }
      this.setTexture(props.backgroundTexture);
    }
    setTexture(backgroundTexture) {
      this.setBindings({
        backgroundTexture
      });
    }
    predraw() {
      this.shaderInputs.setProps({});
      super.predraw();
    }
  };

  // ../../node_modules/@math.gl/core/dist/lib/common.js
  var RADIANS_TO_DEGREES = 1 / Math.PI * 180;
  var DEGREES_TO_RADIANS = 1 / 180 * Math.PI;
  var DEFAULT_CONFIG = {
    EPSILON: 1e-12,
    debug: false,
    precision: 4,
    printTypes: false,
    printDegrees: false,
    printRowMajor: true,
    _cartographicRadians: false
  };
  globalThis.mathgl = globalThis.mathgl || { config: { ...DEFAULT_CONFIG } };
  var config = globalThis.mathgl.config;
  function formatValue(value, { precision = config.precision } = {}) {
    value = round(value);
    return `${parseFloat(value.toPrecision(precision))}`;
  }
  function isArray(value) {
    return Array.isArray(value) || ArrayBuffer.isView(value) && !(value instanceof DataView);
  }
  function equals(a, b, epsilon) {
    const oldEpsilon = config.EPSILON;
    if (epsilon) {
      config.EPSILON = epsilon;
    }
    try {
      if (a === b) {
        return true;
      }
      if (isArray(a) && isArray(b)) {
        if (a.length !== b.length) {
          return false;
        }
        for (let i = 0; i < a.length; ++i) {
          if (!equals(a[i], b[i])) {
            return false;
          }
        }
        return true;
      }
      if (a && a.equals) {
        return a.equals(b);
      }
      if (b && b.equals) {
        return b.equals(a);
      }
      if (typeof a === "number" && typeof b === "number") {
        return Math.abs(a - b) <= config.EPSILON * Math.max(1, Math.abs(a), Math.abs(b));
      }
      return false;
    } finally {
      config.EPSILON = oldEpsilon;
    }
  }
  function round(value) {
    return Math.round(value / config.EPSILON) * config.EPSILON;
  }

  // ../../node_modules/@math.gl/core/dist/classes/base/math-array.js
  var MathArray = class extends Array {
    // Common methods
    /**
     * Clone the current object
     * @returns a new copy of this object
     */
    clone() {
      return new this.constructor().copy(this);
    }
    fromArray(array, offset = 0) {
      for (let i = 0; i < this.ELEMENTS; ++i) {
        this[i] = array[i + offset];
      }
      return this.check();
    }
    toArray(targetArray = [], offset = 0) {
      for (let i = 0; i < this.ELEMENTS; ++i) {
        targetArray[offset + i] = this[i];
      }
      return targetArray;
    }
    toObject(targetObject) {
      return targetObject;
    }
    from(arrayOrObject) {
      return Array.isArray(arrayOrObject) ? this.copy(arrayOrObject) : (
        // @ts-ignore
        this.fromObject(arrayOrObject)
      );
    }
    to(arrayOrObject) {
      if (arrayOrObject === this) {
        return this;
      }
      return isArray(arrayOrObject) ? this.toArray(arrayOrObject) : this.toObject(arrayOrObject);
    }
    toTarget(target) {
      return target ? this.to(target) : this;
    }
    /** @deprecated */
    toFloat32Array() {
      return new Float32Array(this);
    }
    toString() {
      return this.formatString(config);
    }
    /** Formats string according to options */
    formatString(opts) {
      let string = "";
      for (let i = 0; i < this.ELEMENTS; ++i) {
        string += (i > 0 ? ", " : "") + formatValue(this[i], opts);
      }
      return `${opts.printTypes ? this.constructor.name : ""}[${string}]`;
    }
    equals(array) {
      if (!array || this.length !== array.length) {
        return false;
      }
      for (let i = 0; i < this.ELEMENTS; ++i) {
        if (!equals(this[i], array[i])) {
          return false;
        }
      }
      return true;
    }
    exactEquals(array) {
      if (!array || this.length !== array.length) {
        return false;
      }
      for (let i = 0; i < this.ELEMENTS; ++i) {
        if (this[i] !== array[i]) {
          return false;
        }
      }
      return true;
    }
    // Modifiers
    /** Negates all values in this object */
    negate() {
      for (let i = 0; i < this.ELEMENTS; ++i) {
        this[i] = -this[i];
      }
      return this.check();
    }
    lerp(a, b, t) {
      if (t === void 0) {
        return this.lerp(this, a, b);
      }
      for (let i = 0; i < this.ELEMENTS; ++i) {
        const ai = a[i];
        const endValue = typeof b === "number" ? b : b[i];
        this[i] = ai + t * (endValue - ai);
      }
      return this.check();
    }
    /** Minimal */
    min(vector) {
      for (let i = 0; i < this.ELEMENTS; ++i) {
        this[i] = Math.min(vector[i], this[i]);
      }
      return this.check();
    }
    /** Maximal */
    max(vector) {
      for (let i = 0; i < this.ELEMENTS; ++i) {
        this[i] = Math.max(vector[i], this[i]);
      }
      return this.check();
    }
    clamp(minVector, maxVector) {
      for (let i = 0; i < this.ELEMENTS; ++i) {
        this[i] = Math.min(Math.max(this[i], minVector[i]), maxVector[i]);
      }
      return this.check();
    }
    add(...vectors) {
      for (const vector of vectors) {
        for (let i = 0; i < this.ELEMENTS; ++i) {
          this[i] += vector[i];
        }
      }
      return this.check();
    }
    subtract(...vectors) {
      for (const vector of vectors) {
        for (let i = 0; i < this.ELEMENTS; ++i) {
          this[i] -= vector[i];
        }
      }
      return this.check();
    }
    scale(scale2) {
      if (typeof scale2 === "number") {
        for (let i = 0; i < this.ELEMENTS; ++i) {
          this[i] *= scale2;
        }
      } else {
        for (let i = 0; i < this.ELEMENTS && i < scale2.length; ++i) {
          this[i] *= scale2[i];
        }
      }
      return this.check();
    }
    /**
     * Multiplies all elements by `scale`
     * Note: `Matrix4.multiplyByScalar` only scales its 3x3 "minor"
     */
    multiplyByScalar(scalar) {
      for (let i = 0; i < this.ELEMENTS; ++i) {
        this[i] *= scalar;
      }
      return this.check();
    }
    // Debug checks
    /** Throws an error if array length is incorrect or contains illegal values */
    check() {
      if (config.debug && !this.validate()) {
        throw new Error(`math.gl: ${this.constructor.name} some fields set to invalid numbers'`);
      }
      return this;
    }
    /** Returns false if the array length is incorrect or contains illegal values */
    validate() {
      let valid = this.length === this.ELEMENTS;
      for (let i = 0; i < this.ELEMENTS; ++i) {
        valid = valid && Number.isFinite(this[i]);
      }
      return valid;
    }
    // three.js compatibility
    /** @deprecated */
    sub(a) {
      return this.subtract(a);
    }
    /** @deprecated */
    setScalar(a) {
      for (let i = 0; i < this.ELEMENTS; ++i) {
        this[i] = a;
      }
      return this.check();
    }
    /** @deprecated */
    addScalar(a) {
      for (let i = 0; i < this.ELEMENTS; ++i) {
        this[i] += a;
      }
      return this.check();
    }
    /** @deprecated */
    subScalar(a) {
      return this.addScalar(-a);
    }
    /** @deprecated */
    multiplyScalar(scalar) {
      for (let i = 0; i < this.ELEMENTS; ++i) {
        this[i] *= scalar;
      }
      return this.check();
    }
    /** @deprecated */
    divideScalar(a) {
      return this.multiplyByScalar(1 / a);
    }
    /** @deprecated */
    clampScalar(min, max) {
      for (let i = 0; i < this.ELEMENTS; ++i) {
        this[i] = Math.min(Math.max(this[i], min), max);
      }
      return this.check();
    }
    /** @deprecated */
    get elements() {
      return this;
    }
  };

  // ../../node_modules/@math.gl/core/dist/lib/validators.js
  function validateVector(v, length) {
    if (v.length !== length) {
      return false;
    }
    for (let i = 0; i < v.length; ++i) {
      if (!Number.isFinite(v[i])) {
        return false;
      }
    }
    return true;
  }
  function checkNumber(value) {
    if (!Number.isFinite(value)) {
      throw new Error(`Invalid number ${JSON.stringify(value)}`);
    }
    return value;
  }
  function checkVector(v, length, callerName = "") {
    if (config.debug && !validateVector(v, length)) {
      throw new Error(`math.gl: ${callerName} some fields set to invalid numbers'`);
    }
    return v;
  }

  // ../../node_modules/@math.gl/core/dist/lib/assert.js
  function assert(condition, message) {
    if (!condition) {
      throw new Error(`math.gl assertion ${message}`);
    }
  }

  // ../../node_modules/@math.gl/core/dist/classes/base/vector.js
  var Vector = class extends MathArray {
    // ACCESSORS
    get x() {
      return this[0];
    }
    set x(value) {
      this[0] = checkNumber(value);
    }
    get y() {
      return this[1];
    }
    set y(value) {
      this[1] = checkNumber(value);
    }
    /**
     * Returns the length of the vector from the origin to the point described by this vector
     *
     * @note `length` is a reserved word for Arrays, so `v.length()` will return number of elements
     * Instead we provide `len` and `magnitude`
     */
    len() {
      return Math.sqrt(this.lengthSquared());
    }
    /**
     * Returns the length of the vector from the origin to the point described by this vector
     */
    magnitude() {
      return this.len();
    }
    /**
     * Returns the squared length of the vector from the origin to the point described by this vector
     */
    lengthSquared() {
      let length = 0;
      for (let i = 0; i < this.ELEMENTS; ++i) {
        length += this[i] * this[i];
      }
      return length;
    }
    /**
     * Returns the squared length of the vector from the origin to the point described by this vector
     */
    magnitudeSquared() {
      return this.lengthSquared();
    }
    distance(mathArray) {
      return Math.sqrt(this.distanceSquared(mathArray));
    }
    distanceSquared(mathArray) {
      let length = 0;
      for (let i = 0; i < this.ELEMENTS; ++i) {
        const dist = this[i] - mathArray[i];
        length += dist * dist;
      }
      return checkNumber(length);
    }
    dot(mathArray) {
      let product = 0;
      for (let i = 0; i < this.ELEMENTS; ++i) {
        product += this[i] * mathArray[i];
      }
      return checkNumber(product);
    }
    // MODIFIERS
    normalize() {
      const length = this.magnitude();
      if (length !== 0) {
        for (let i = 0; i < this.ELEMENTS; ++i) {
          this[i] /= length;
        }
      }
      return this.check();
    }
    multiply(...vectors) {
      for (const vector of vectors) {
        for (let i = 0; i < this.ELEMENTS; ++i) {
          this[i] *= vector[i];
        }
      }
      return this.check();
    }
    divide(...vectors) {
      for (const vector of vectors) {
        for (let i = 0; i < this.ELEMENTS; ++i) {
          this[i] /= vector[i];
        }
      }
      return this.check();
    }
    // THREE.js compatibility
    lengthSq() {
      return this.lengthSquared();
    }
    distanceTo(vector) {
      return this.distance(vector);
    }
    distanceToSquared(vector) {
      return this.distanceSquared(vector);
    }
    getComponent(i) {
      assert(i >= 0 && i < this.ELEMENTS, "index is out of range");
      return checkNumber(this[i]);
    }
    setComponent(i, value) {
      assert(i >= 0 && i < this.ELEMENTS, "index is out of range");
      this[i] = value;
      return this.check();
    }
    addVectors(a, b) {
      return this.copy(a).add(b);
    }
    subVectors(a, b) {
      return this.copy(a).subtract(b);
    }
    multiplyVectors(a, b) {
      return this.copy(a).multiply(b);
    }
    addScaledVector(a, b) {
      return this.add(new this.constructor(a).multiplyScalar(b));
    }
  };

  // ../../node_modules/@math.gl/core/dist/gl-matrix/common.js
  var EPSILON = 1e-6;
  var ARRAY_TYPE = typeof Float32Array !== "undefined" ? Float32Array : Array;
  var degree = Math.PI / 180;

  // ../../node_modules/@math.gl/core/dist/gl-matrix/vec2.js
  function create() {
    const out = new ARRAY_TYPE(2);
    if (ARRAY_TYPE != Float32Array) {
      out[0] = 0;
      out[1] = 0;
    }
    return out;
  }
  function transformMat4(out, a, m) {
    const x = a[0];
    const y = a[1];
    out[0] = m[0] * x + m[4] * y + m[12];
    out[1] = m[1] * x + m[5] * y + m[13];
    return out;
  }
  var forEach = function() {
    const vec = create();
    return function(a, stride, offset, count, fn, arg) {
      let i;
      let l;
      if (!stride) {
        stride = 2;
      }
      if (!offset) {
        offset = 0;
      }
      if (count) {
        l = Math.min(count * stride + offset, a.length);
      } else {
        l = a.length;
      }
      for (i = offset; i < l; i += stride) {
        vec[0] = a[i];
        vec[1] = a[i + 1];
        fn(vec, vec, arg);
        a[i] = vec[0];
        a[i + 1] = vec[1];
      }
      return a;
    };
  }();

  // ../../node_modules/@math.gl/core/dist/lib/gl-matrix-extras.js
  function vec2_transformMat4AsVector(out, a, m) {
    const x = a[0];
    const y = a[1];
    const w = m[3] * x + m[7] * y || 1;
    out[0] = (m[0] * x + m[4] * y) / w;
    out[1] = (m[1] * x + m[5] * y) / w;
    return out;
  }
  function vec3_transformMat4AsVector(out, a, m) {
    const x = a[0];
    const y = a[1];
    const z = a[2];
    const w = m[3] * x + m[7] * y + m[11] * z || 1;
    out[0] = (m[0] * x + m[4] * y + m[8] * z) / w;
    out[1] = (m[1] * x + m[5] * y + m[9] * z) / w;
    out[2] = (m[2] * x + m[6] * y + m[10] * z) / w;
    return out;
  }
  function vec3_transformMat2(out, a, m) {
    const x = a[0];
    const y = a[1];
    out[0] = m[0] * x + m[2] * y;
    out[1] = m[1] * x + m[3] * y;
    out[2] = a[2];
    return out;
  }

  // ../../node_modules/@math.gl/core/dist/gl-matrix/vec3.js
  function create2() {
    const out = new ARRAY_TYPE(3);
    if (ARRAY_TYPE != Float32Array) {
      out[0] = 0;
      out[1] = 0;
      out[2] = 0;
    }
    return out;
  }
  function dot(a, b) {
    return a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
  }
  function cross(out, a, b) {
    const ax = a[0];
    const ay = a[1];
    const az = a[2];
    const bx = b[0];
    const by = b[1];
    const bz = b[2];
    out[0] = ay * bz - az * by;
    out[1] = az * bx - ax * bz;
    out[2] = ax * by - ay * bx;
    return out;
  }
  function transformMat42(out, a, m) {
    const x = a[0];
    const y = a[1];
    const z = a[2];
    let w = m[3] * x + m[7] * y + m[11] * z + m[15];
    w = w || 1;
    out[0] = (m[0] * x + m[4] * y + m[8] * z + m[12]) / w;
    out[1] = (m[1] * x + m[5] * y + m[9] * z + m[13]) / w;
    out[2] = (m[2] * x + m[6] * y + m[10] * z + m[14]) / w;
    return out;
  }
  function transformMat3(out, a, m) {
    const x = a[0];
    const y = a[1];
    const z = a[2];
    out[0] = x * m[0] + y * m[3] + z * m[6];
    out[1] = x * m[1] + y * m[4] + z * m[7];
    out[2] = x * m[2] + y * m[5] + z * m[8];
    return out;
  }
  function transformQuat(out, a, q) {
    const qx = q[0];
    const qy = q[1];
    const qz = q[2];
    const qw = q[3];
    const x = a[0];
    const y = a[1];
    const z = a[2];
    let uvx = qy * z - qz * y;
    let uvy = qz * x - qx * z;
    let uvz = qx * y - qy * x;
    let uuvx = qy * uvz - qz * uvy;
    let uuvy = qz * uvx - qx * uvz;
    let uuvz = qx * uvy - qy * uvx;
    const w2 = qw * 2;
    uvx *= w2;
    uvy *= w2;
    uvz *= w2;
    uuvx *= 2;
    uuvy *= 2;
    uuvz *= 2;
    out[0] = x + uvx + uuvx;
    out[1] = y + uvy + uuvy;
    out[2] = z + uvz + uuvz;
    return out;
  }
  function rotateX(out, a, b, rad) {
    const p = [];
    const r = [];
    p[0] = a[0] - b[0];
    p[1] = a[1] - b[1];
    p[2] = a[2] - b[2];
    r[0] = p[0];
    r[1] = p[1] * Math.cos(rad) - p[2] * Math.sin(rad);
    r[2] = p[1] * Math.sin(rad) + p[2] * Math.cos(rad);
    out[0] = r[0] + b[0];
    out[1] = r[1] + b[1];
    out[2] = r[2] + b[2];
    return out;
  }
  function rotateY(out, a, b, rad) {
    const p = [];
    const r = [];
    p[0] = a[0] - b[0];
    p[1] = a[1] - b[1];
    p[2] = a[2] - b[2];
    r[0] = p[2] * Math.sin(rad) + p[0] * Math.cos(rad);
    r[1] = p[1];
    r[2] = p[2] * Math.cos(rad) - p[0] * Math.sin(rad);
    out[0] = r[0] + b[0];
    out[1] = r[1] + b[1];
    out[2] = r[2] + b[2];
    return out;
  }
  function rotateZ(out, a, b, rad) {
    const p = [];
    const r = [];
    p[0] = a[0] - b[0];
    p[1] = a[1] - b[1];
    p[2] = a[2] - b[2];
    r[0] = p[0] * Math.cos(rad) - p[1] * Math.sin(rad);
    r[1] = p[0] * Math.sin(rad) + p[1] * Math.cos(rad);
    r[2] = p[2];
    out[0] = r[0] + b[0];
    out[1] = r[1] + b[1];
    out[2] = r[2] + b[2];
    return out;
  }
  function angle(a, b) {
    const ax = a[0];
    const ay = a[1];
    const az = a[2];
    const bx = b[0];
    const by = b[1];
    const bz = b[2];
    const mag = Math.sqrt((ax * ax + ay * ay + az * az) * (bx * bx + by * by + bz * bz));
    const cosine = mag && dot(a, b) / mag;
    return Math.acos(Math.min(Math.max(cosine, -1), 1));
  }
  var forEach2 = function() {
    const vec = create2();
    return function(a, stride, offset, count, fn, arg) {
      let i;
      let l;
      if (!stride) {
        stride = 3;
      }
      if (!offset) {
        offset = 0;
      }
      if (count) {
        l = Math.min(count * stride + offset, a.length);
      } else {
        l = a.length;
      }
      for (i = offset; i < l; i += stride) {
        vec[0] = a[i];
        vec[1] = a[i + 1];
        vec[2] = a[i + 2];
        fn(vec, vec, arg);
        a[i] = vec[0];
        a[i + 1] = vec[1];
        a[i + 2] = vec[2];
      }
      return a;
    };
  }();

  // ../../node_modules/@math.gl/core/dist/classes/vector3.js
  var ORIGIN = [0, 0, 0];
  var ZERO;
  var Vector3 = class extends Vector {
    static get ZERO() {
      if (!ZERO) {
        ZERO = new Vector3(0, 0, 0);
        Object.freeze(ZERO);
      }
      return ZERO;
    }
    /**
     * @class
     * @param x
     * @param y
     * @param z
     */
    constructor(x = 0, y = 0, z = 0) {
      super(-0, -0, -0);
      if (arguments.length === 1 && isArray(x)) {
        this.copy(x);
      } else {
        if (config.debug) {
          checkNumber(x);
          checkNumber(y);
          checkNumber(z);
        }
        this[0] = x;
        this[1] = y;
        this[2] = z;
      }
    }
    set(x, y, z) {
      this[0] = x;
      this[1] = y;
      this[2] = z;
      return this.check();
    }
    copy(array) {
      this[0] = array[0];
      this[1] = array[1];
      this[2] = array[2];
      return this.check();
    }
    fromObject(object) {
      if (config.debug) {
        checkNumber(object.x);
        checkNumber(object.y);
        checkNumber(object.z);
      }
      this[0] = object.x;
      this[1] = object.y;
      this[2] = object.z;
      return this.check();
    }
    toObject(object) {
      object.x = this[0];
      object.y = this[1];
      object.z = this[2];
      return object;
    }
    // Getters/setters
    get ELEMENTS() {
      return 3;
    }
    get z() {
      return this[2];
    }
    set z(value) {
      this[2] = checkNumber(value);
    }
    // ACCESSORS
    angle(vector) {
      return angle(this, vector);
    }
    // MODIFIERS
    cross(vector) {
      cross(this, this, vector);
      return this.check();
    }
    rotateX({ radians, origin = ORIGIN }) {
      rotateX(this, this, origin, radians);
      return this.check();
    }
    rotateY({ radians, origin = ORIGIN }) {
      rotateY(this, this, origin, radians);
      return this.check();
    }
    rotateZ({ radians, origin = ORIGIN }) {
      rotateZ(this, this, origin, radians);
      return this.check();
    }
    // Transforms
    // transforms as point (4th component is implicitly 1)
    transform(matrix4) {
      return this.transformAsPoint(matrix4);
    }
    // transforms as point (4th component is implicitly 1)
    transformAsPoint(matrix4) {
      transformMat42(this, this, matrix4);
      return this.check();
    }
    // transforms as vector  (4th component is implicitly 0, ignores translation. slightly faster)
    transformAsVector(matrix4) {
      vec3_transformMat4AsVector(this, this, matrix4);
      return this.check();
    }
    transformByMatrix3(matrix3) {
      transformMat3(this, this, matrix3);
      return this.check();
    }
    transformByMatrix2(matrix2) {
      vec3_transformMat2(this, this, matrix2);
      return this.check();
    }
    transformByQuaternion(quaternion) {
      transformQuat(this, this, quaternion);
      return this.check();
    }
  };

  // ../../node_modules/@math.gl/core/dist/classes/base/matrix.js
  var Matrix = class extends MathArray {
    // fromObject(object) {
    //   const array = object.elements;
    //   return this.fromRowMajor(array);
    // }
    // toObject(object) {
    //   const array = object.elements;
    //   this.toRowMajor(array);
    //   return object;
    // }
    // TODO better override formatString?
    toString() {
      let string = "[";
      if (config.printRowMajor) {
        string += "row-major:";
        for (let row = 0; row < this.RANK; ++row) {
          for (let col = 0; col < this.RANK; ++col) {
            string += ` ${this[col * this.RANK + row]}`;
          }
        }
      } else {
        string += "column-major:";
        for (let i = 0; i < this.ELEMENTS; ++i) {
          string += ` ${this[i]}`;
        }
      }
      string += "]";
      return string;
    }
    getElementIndex(row, col) {
      return col * this.RANK + row;
    }
    // By default assumes row major indices
    getElement(row, col) {
      return this[col * this.RANK + row];
    }
    // By default assumes row major indices
    setElement(row, col, value) {
      this[col * this.RANK + row] = checkNumber(value);
      return this;
    }
    getColumn(columnIndex, result = new Array(this.RANK).fill(-0)) {
      const firstIndex = columnIndex * this.RANK;
      for (let i = 0; i < this.RANK; ++i) {
        result[i] = this[firstIndex + i];
      }
      return result;
    }
    setColumn(columnIndex, columnVector) {
      const firstIndex = columnIndex * this.RANK;
      for (let i = 0; i < this.RANK; ++i) {
        this[firstIndex + i] = columnVector[i];
      }
      return this;
    }
  };

  // ../../node_modules/@math.gl/core/dist/gl-matrix/mat4.js
  function identity(out) {
    out[0] = 1;
    out[1] = 0;
    out[2] = 0;
    out[3] = 0;
    out[4] = 0;
    out[5] = 1;
    out[6] = 0;
    out[7] = 0;
    out[8] = 0;
    out[9] = 0;
    out[10] = 1;
    out[11] = 0;
    out[12] = 0;
    out[13] = 0;
    out[14] = 0;
    out[15] = 1;
    return out;
  }
  function transpose(out, a) {
    if (out === a) {
      const a01 = a[1];
      const a02 = a[2];
      const a03 = a[3];
      const a12 = a[6];
      const a13 = a[7];
      const a23 = a[11];
      out[1] = a[4];
      out[2] = a[8];
      out[3] = a[12];
      out[4] = a01;
      out[6] = a[9];
      out[7] = a[13];
      out[8] = a02;
      out[9] = a12;
      out[11] = a[14];
      out[12] = a03;
      out[13] = a13;
      out[14] = a23;
    } else {
      out[0] = a[0];
      out[1] = a[4];
      out[2] = a[8];
      out[3] = a[12];
      out[4] = a[1];
      out[5] = a[5];
      out[6] = a[9];
      out[7] = a[13];
      out[8] = a[2];
      out[9] = a[6];
      out[10] = a[10];
      out[11] = a[14];
      out[12] = a[3];
      out[13] = a[7];
      out[14] = a[11];
      out[15] = a[15];
    }
    return out;
  }
  function invert(out, a) {
    const a00 = a[0];
    const a01 = a[1];
    const a02 = a[2];
    const a03 = a[3];
    const a10 = a[4];
    const a11 = a[5];
    const a12 = a[6];
    const a13 = a[7];
    const a20 = a[8];
    const a21 = a[9];
    const a22 = a[10];
    const a23 = a[11];
    const a30 = a[12];
    const a31 = a[13];
    const a32 = a[14];
    const a33 = a[15];
    const b00 = a00 * a11 - a01 * a10;
    const b01 = a00 * a12 - a02 * a10;
    const b02 = a00 * a13 - a03 * a10;
    const b03 = a01 * a12 - a02 * a11;
    const b04 = a01 * a13 - a03 * a11;
    const b05 = a02 * a13 - a03 * a12;
    const b06 = a20 * a31 - a21 * a30;
    const b07 = a20 * a32 - a22 * a30;
    const b08 = a20 * a33 - a23 * a30;
    const b09 = a21 * a32 - a22 * a31;
    const b10 = a21 * a33 - a23 * a31;
    const b11 = a22 * a33 - a23 * a32;
    let det = b00 * b11 - b01 * b10 + b02 * b09 + b03 * b08 - b04 * b07 + b05 * b06;
    if (!det) {
      return null;
    }
    det = 1 / det;
    out[0] = (a11 * b11 - a12 * b10 + a13 * b09) * det;
    out[1] = (a02 * b10 - a01 * b11 - a03 * b09) * det;
    out[2] = (a31 * b05 - a32 * b04 + a33 * b03) * det;
    out[3] = (a22 * b04 - a21 * b05 - a23 * b03) * det;
    out[4] = (a12 * b08 - a10 * b11 - a13 * b07) * det;
    out[5] = (a00 * b11 - a02 * b08 + a03 * b07) * det;
    out[6] = (a32 * b02 - a30 * b05 - a33 * b01) * det;
    out[7] = (a20 * b05 - a22 * b02 + a23 * b01) * det;
    out[8] = (a10 * b10 - a11 * b08 + a13 * b06) * det;
    out[9] = (a01 * b08 - a00 * b10 - a03 * b06) * det;
    out[10] = (a30 * b04 - a31 * b02 + a33 * b00) * det;
    out[11] = (a21 * b02 - a20 * b04 - a23 * b00) * det;
    out[12] = (a11 * b07 - a10 * b09 - a12 * b06) * det;
    out[13] = (a00 * b09 - a01 * b07 + a02 * b06) * det;
    out[14] = (a31 * b01 - a30 * b03 - a32 * b00) * det;
    out[15] = (a20 * b03 - a21 * b01 + a22 * b00) * det;
    return out;
  }
  function determinant(a) {
    const a00 = a[0];
    const a01 = a[1];
    const a02 = a[2];
    const a03 = a[3];
    const a10 = a[4];
    const a11 = a[5];
    const a12 = a[6];
    const a13 = a[7];
    const a20 = a[8];
    const a21 = a[9];
    const a22 = a[10];
    const a23 = a[11];
    const a30 = a[12];
    const a31 = a[13];
    const a32 = a[14];
    const a33 = a[15];
    const b0 = a00 * a11 - a01 * a10;
    const b1 = a00 * a12 - a02 * a10;
    const b2 = a01 * a12 - a02 * a11;
    const b3 = a20 * a31 - a21 * a30;
    const b4 = a20 * a32 - a22 * a30;
    const b5 = a21 * a32 - a22 * a31;
    const b6 = a00 * b5 - a01 * b4 + a02 * b3;
    const b7 = a10 * b5 - a11 * b4 + a12 * b3;
    const b8 = a20 * b2 - a21 * b1 + a22 * b0;
    const b9 = a30 * b2 - a31 * b1 + a32 * b0;
    return a13 * b6 - a03 * b7 + a33 * b8 - a23 * b9;
  }
  function multiply(out, a, b) {
    const a00 = a[0];
    const a01 = a[1];
    const a02 = a[2];
    const a03 = a[3];
    const a10 = a[4];
    const a11 = a[5];
    const a12 = a[6];
    const a13 = a[7];
    const a20 = a[8];
    const a21 = a[9];
    const a22 = a[10];
    const a23 = a[11];
    const a30 = a[12];
    const a31 = a[13];
    const a32 = a[14];
    const a33 = a[15];
    let b0 = b[0];
    let b1 = b[1];
    let b2 = b[2];
    let b3 = b[3];
    out[0] = b0 * a00 + b1 * a10 + b2 * a20 + b3 * a30;
    out[1] = b0 * a01 + b1 * a11 + b2 * a21 + b3 * a31;
    out[2] = b0 * a02 + b1 * a12 + b2 * a22 + b3 * a32;
    out[3] = b0 * a03 + b1 * a13 + b2 * a23 + b3 * a33;
    b0 = b[4];
    b1 = b[5];
    b2 = b[6];
    b3 = b[7];
    out[4] = b0 * a00 + b1 * a10 + b2 * a20 + b3 * a30;
    out[5] = b0 * a01 + b1 * a11 + b2 * a21 + b3 * a31;
    out[6] = b0 * a02 + b1 * a12 + b2 * a22 + b3 * a32;
    out[7] = b0 * a03 + b1 * a13 + b2 * a23 + b3 * a33;
    b0 = b[8];
    b1 = b[9];
    b2 = b[10];
    b3 = b[11];
    out[8] = b0 * a00 + b1 * a10 + b2 * a20 + b3 * a30;
    out[9] = b0 * a01 + b1 * a11 + b2 * a21 + b3 * a31;
    out[10] = b0 * a02 + b1 * a12 + b2 * a22 + b3 * a32;
    out[11] = b0 * a03 + b1 * a13 + b2 * a23 + b3 * a33;
    b0 = b[12];
    b1 = b[13];
    b2 = b[14];
    b3 = b[15];
    out[12] = b0 * a00 + b1 * a10 + b2 * a20 + b3 * a30;
    out[13] = b0 * a01 + b1 * a11 + b2 * a21 + b3 * a31;
    out[14] = b0 * a02 + b1 * a12 + b2 * a22 + b3 * a32;
    out[15] = b0 * a03 + b1 * a13 + b2 * a23 + b3 * a33;
    return out;
  }
  function translate(out, a, v) {
    const x = v[0];
    const y = v[1];
    const z = v[2];
    let a00;
    let a01;
    let a02;
    let a03;
    let a10;
    let a11;
    let a12;
    let a13;
    let a20;
    let a21;
    let a22;
    let a23;
    if (a === out) {
      out[12] = a[0] * x + a[4] * y + a[8] * z + a[12];
      out[13] = a[1] * x + a[5] * y + a[9] * z + a[13];
      out[14] = a[2] * x + a[6] * y + a[10] * z + a[14];
      out[15] = a[3] * x + a[7] * y + a[11] * z + a[15];
    } else {
      a00 = a[0];
      a01 = a[1];
      a02 = a[2];
      a03 = a[3];
      a10 = a[4];
      a11 = a[5];
      a12 = a[6];
      a13 = a[7];
      a20 = a[8];
      a21 = a[9];
      a22 = a[10];
      a23 = a[11];
      out[0] = a00;
      out[1] = a01;
      out[2] = a02;
      out[3] = a03;
      out[4] = a10;
      out[5] = a11;
      out[6] = a12;
      out[7] = a13;
      out[8] = a20;
      out[9] = a21;
      out[10] = a22;
      out[11] = a23;
      out[12] = a00 * x + a10 * y + a20 * z + a[12];
      out[13] = a01 * x + a11 * y + a21 * z + a[13];
      out[14] = a02 * x + a12 * y + a22 * z + a[14];
      out[15] = a03 * x + a13 * y + a23 * z + a[15];
    }
    return out;
  }
  function scale(out, a, v) {
    const x = v[0];
    const y = v[1];
    const z = v[2];
    out[0] = a[0] * x;
    out[1] = a[1] * x;
    out[2] = a[2] * x;
    out[3] = a[3] * x;
    out[4] = a[4] * y;
    out[5] = a[5] * y;
    out[6] = a[6] * y;
    out[7] = a[7] * y;
    out[8] = a[8] * z;
    out[9] = a[9] * z;
    out[10] = a[10] * z;
    out[11] = a[11] * z;
    out[12] = a[12];
    out[13] = a[13];
    out[14] = a[14];
    out[15] = a[15];
    return out;
  }
  function rotate(out, a, rad, axis) {
    let x = axis[0];
    let y = axis[1];
    let z = axis[2];
    let len = Math.sqrt(x * x + y * y + z * z);
    let c;
    let s;
    let t;
    let a00;
    let a01;
    let a02;
    let a03;
    let a10;
    let a11;
    let a12;
    let a13;
    let a20;
    let a21;
    let a22;
    let a23;
    let b00;
    let b01;
    let b02;
    let b10;
    let b11;
    let b12;
    let b20;
    let b21;
    let b22;
    if (len < EPSILON) {
      return null;
    }
    len = 1 / len;
    x *= len;
    y *= len;
    z *= len;
    s = Math.sin(rad);
    c = Math.cos(rad);
    t = 1 - c;
    a00 = a[0];
    a01 = a[1];
    a02 = a[2];
    a03 = a[3];
    a10 = a[4];
    a11 = a[5];
    a12 = a[6];
    a13 = a[7];
    a20 = a[8];
    a21 = a[9];
    a22 = a[10];
    a23 = a[11];
    b00 = x * x * t + c;
    b01 = y * x * t + z * s;
    b02 = z * x * t - y * s;
    b10 = x * y * t - z * s;
    b11 = y * y * t + c;
    b12 = z * y * t + x * s;
    b20 = x * z * t + y * s;
    b21 = y * z * t - x * s;
    b22 = z * z * t + c;
    out[0] = a00 * b00 + a10 * b01 + a20 * b02;
    out[1] = a01 * b00 + a11 * b01 + a21 * b02;
    out[2] = a02 * b00 + a12 * b01 + a22 * b02;
    out[3] = a03 * b00 + a13 * b01 + a23 * b02;
    out[4] = a00 * b10 + a10 * b11 + a20 * b12;
    out[5] = a01 * b10 + a11 * b11 + a21 * b12;
    out[6] = a02 * b10 + a12 * b11 + a22 * b12;
    out[7] = a03 * b10 + a13 * b11 + a23 * b12;
    out[8] = a00 * b20 + a10 * b21 + a20 * b22;
    out[9] = a01 * b20 + a11 * b21 + a21 * b22;
    out[10] = a02 * b20 + a12 * b21 + a22 * b22;
    out[11] = a03 * b20 + a13 * b21 + a23 * b22;
    if (a !== out) {
      out[12] = a[12];
      out[13] = a[13];
      out[14] = a[14];
      out[15] = a[15];
    }
    return out;
  }
  function rotateX2(out, a, rad) {
    const s = Math.sin(rad);
    const c = Math.cos(rad);
    const a10 = a[4];
    const a11 = a[5];
    const a12 = a[6];
    const a13 = a[7];
    const a20 = a[8];
    const a21 = a[9];
    const a22 = a[10];
    const a23 = a[11];
    if (a !== out) {
      out[0] = a[0];
      out[1] = a[1];
      out[2] = a[2];
      out[3] = a[3];
      out[12] = a[12];
      out[13] = a[13];
      out[14] = a[14];
      out[15] = a[15];
    }
    out[4] = a10 * c + a20 * s;
    out[5] = a11 * c + a21 * s;
    out[6] = a12 * c + a22 * s;
    out[7] = a13 * c + a23 * s;
    out[8] = a20 * c - a10 * s;
    out[9] = a21 * c - a11 * s;
    out[10] = a22 * c - a12 * s;
    out[11] = a23 * c - a13 * s;
    return out;
  }
  function rotateY2(out, a, rad) {
    const s = Math.sin(rad);
    const c = Math.cos(rad);
    const a00 = a[0];
    const a01 = a[1];
    const a02 = a[2];
    const a03 = a[3];
    const a20 = a[8];
    const a21 = a[9];
    const a22 = a[10];
    const a23 = a[11];
    if (a !== out) {
      out[4] = a[4];
      out[5] = a[5];
      out[6] = a[6];
      out[7] = a[7];
      out[12] = a[12];
      out[13] = a[13];
      out[14] = a[14];
      out[15] = a[15];
    }
    out[0] = a00 * c - a20 * s;
    out[1] = a01 * c - a21 * s;
    out[2] = a02 * c - a22 * s;
    out[3] = a03 * c - a23 * s;
    out[8] = a00 * s + a20 * c;
    out[9] = a01 * s + a21 * c;
    out[10] = a02 * s + a22 * c;
    out[11] = a03 * s + a23 * c;
    return out;
  }
  function rotateZ2(out, a, rad) {
    const s = Math.sin(rad);
    const c = Math.cos(rad);
    const a00 = a[0];
    const a01 = a[1];
    const a02 = a[2];
    const a03 = a[3];
    const a10 = a[4];
    const a11 = a[5];
    const a12 = a[6];
    const a13 = a[7];
    if (a !== out) {
      out[8] = a[8];
      out[9] = a[9];
      out[10] = a[10];
      out[11] = a[11];
      out[12] = a[12];
      out[13] = a[13];
      out[14] = a[14];
      out[15] = a[15];
    }
    out[0] = a00 * c + a10 * s;
    out[1] = a01 * c + a11 * s;
    out[2] = a02 * c + a12 * s;
    out[3] = a03 * c + a13 * s;
    out[4] = a10 * c - a00 * s;
    out[5] = a11 * c - a01 * s;
    out[6] = a12 * c - a02 * s;
    out[7] = a13 * c - a03 * s;
    return out;
  }
  function fromQuat(out, q) {
    const x = q[0];
    const y = q[1];
    const z = q[2];
    const w = q[3];
    const x2 = x + x;
    const y2 = y + y;
    const z2 = z + z;
    const xx = x * x2;
    const yx = y * x2;
    const yy = y * y2;
    const zx = z * x2;
    const zy = z * y2;
    const zz = z * z2;
    const wx = w * x2;
    const wy = w * y2;
    const wz = w * z2;
    out[0] = 1 - yy - zz;
    out[1] = yx + wz;
    out[2] = zx - wy;
    out[3] = 0;
    out[4] = yx - wz;
    out[5] = 1 - xx - zz;
    out[6] = zy + wx;
    out[7] = 0;
    out[8] = zx + wy;
    out[9] = zy - wx;
    out[10] = 1 - xx - yy;
    out[11] = 0;
    out[12] = 0;
    out[13] = 0;
    out[14] = 0;
    out[15] = 1;
    return out;
  }
  function frustum(out, left, right, bottom, top, near, far) {
    const rl = 1 / (right - left);
    const tb = 1 / (top - bottom);
    const nf = 1 / (near - far);
    out[0] = near * 2 * rl;
    out[1] = 0;
    out[2] = 0;
    out[3] = 0;
    out[4] = 0;
    out[5] = near * 2 * tb;
    out[6] = 0;
    out[7] = 0;
    out[8] = (right + left) * rl;
    out[9] = (top + bottom) * tb;
    out[10] = (far + near) * nf;
    out[11] = -1;
    out[12] = 0;
    out[13] = 0;
    out[14] = far * near * 2 * nf;
    out[15] = 0;
    return out;
  }
  function perspectiveNO(out, fovy, aspect, near, far) {
    const f = 1 / Math.tan(fovy / 2);
    out[0] = f / aspect;
    out[1] = 0;
    out[2] = 0;
    out[3] = 0;
    out[4] = 0;
    out[5] = f;
    out[6] = 0;
    out[7] = 0;
    out[8] = 0;
    out[9] = 0;
    out[11] = -1;
    out[12] = 0;
    out[13] = 0;
    out[15] = 0;
    if (far != null && far !== Infinity) {
      const nf = 1 / (near - far);
      out[10] = (far + near) * nf;
      out[14] = 2 * far * near * nf;
    } else {
      out[10] = -1;
      out[14] = -2 * near;
    }
    return out;
  }
  var perspective = perspectiveNO;
  function orthoNO(out, left, right, bottom, top, near, far) {
    const lr = 1 / (left - right);
    const bt = 1 / (bottom - top);
    const nf = 1 / (near - far);
    out[0] = -2 * lr;
    out[1] = 0;
    out[2] = 0;
    out[3] = 0;
    out[4] = 0;
    out[5] = -2 * bt;
    out[6] = 0;
    out[7] = 0;
    out[8] = 0;
    out[9] = 0;
    out[10] = 2 * nf;
    out[11] = 0;
    out[12] = (left + right) * lr;
    out[13] = (top + bottom) * bt;
    out[14] = (far + near) * nf;
    out[15] = 1;
    return out;
  }
  var ortho = orthoNO;
  function lookAt(out, eye, center, up) {
    let len;
    let x0;
    let x1;
    let x2;
    let y0;
    let y1;
    let y2;
    let z0;
    let z1;
    let z2;
    const eyex = eye[0];
    const eyey = eye[1];
    const eyez = eye[2];
    const upx = up[0];
    const upy = up[1];
    const upz = up[2];
    const centerx = center[0];
    const centery = center[1];
    const centerz = center[2];
    if (Math.abs(eyex - centerx) < EPSILON && Math.abs(eyey - centery) < EPSILON && Math.abs(eyez - centerz) < EPSILON) {
      return identity(out);
    }
    z0 = eyex - centerx;
    z1 = eyey - centery;
    z2 = eyez - centerz;
    len = 1 / Math.sqrt(z0 * z0 + z1 * z1 + z2 * z2);
    z0 *= len;
    z1 *= len;
    z2 *= len;
    x0 = upy * z2 - upz * z1;
    x1 = upz * z0 - upx * z2;
    x2 = upx * z1 - upy * z0;
    len = Math.sqrt(x0 * x0 + x1 * x1 + x2 * x2);
    if (!len) {
      x0 = 0;
      x1 = 0;
      x2 = 0;
    } else {
      len = 1 / len;
      x0 *= len;
      x1 *= len;
      x2 *= len;
    }
    y0 = z1 * x2 - z2 * x1;
    y1 = z2 * x0 - z0 * x2;
    y2 = z0 * x1 - z1 * x0;
    len = Math.sqrt(y0 * y0 + y1 * y1 + y2 * y2);
    if (!len) {
      y0 = 0;
      y1 = 0;
      y2 = 0;
    } else {
      len = 1 / len;
      y0 *= len;
      y1 *= len;
      y2 *= len;
    }
    out[0] = x0;
    out[1] = y0;
    out[2] = z0;
    out[3] = 0;
    out[4] = x1;
    out[5] = y1;
    out[6] = z1;
    out[7] = 0;
    out[8] = x2;
    out[9] = y2;
    out[10] = z2;
    out[11] = 0;
    out[12] = -(x0 * eyex + x1 * eyey + x2 * eyez);
    out[13] = -(y0 * eyex + y1 * eyey + y2 * eyez);
    out[14] = -(z0 * eyex + z1 * eyey + z2 * eyez);
    out[15] = 1;
    return out;
  }

  // ../../node_modules/@math.gl/core/dist/gl-matrix/vec4.js
  function create3() {
    const out = new ARRAY_TYPE(4);
    if (ARRAY_TYPE != Float32Array) {
      out[0] = 0;
      out[1] = 0;
      out[2] = 0;
      out[3] = 0;
    }
    return out;
  }
  function transformMat43(out, a, m) {
    const x = a[0];
    const y = a[1];
    const z = a[2];
    const w = a[3];
    out[0] = m[0] * x + m[4] * y + m[8] * z + m[12] * w;
    out[1] = m[1] * x + m[5] * y + m[9] * z + m[13] * w;
    out[2] = m[2] * x + m[6] * y + m[10] * z + m[14] * w;
    out[3] = m[3] * x + m[7] * y + m[11] * z + m[15] * w;
    return out;
  }
  var forEach3 = function() {
    const vec = create3();
    return function(a, stride, offset, count, fn, arg) {
      let i;
      let l;
      if (!stride) {
        stride = 4;
      }
      if (!offset) {
        offset = 0;
      }
      if (count) {
        l = Math.min(count * stride + offset, a.length);
      } else {
        l = a.length;
      }
      for (i = offset; i < l; i += stride) {
        vec[0] = a[i];
        vec[1] = a[i + 1];
        vec[2] = a[i + 2];
        vec[3] = a[i + 3];
        fn(vec, vec, arg);
        a[i] = vec[0];
        a[i + 1] = vec[1];
        a[i + 2] = vec[2];
        a[i + 3] = vec[3];
      }
      return a;
    };
  }();

  // ../../node_modules/@math.gl/core/dist/classes/matrix4.js
  var INDICES;
  (function(INDICES2) {
    INDICES2[INDICES2["COL0ROW0"] = 0] = "COL0ROW0";
    INDICES2[INDICES2["COL0ROW1"] = 1] = "COL0ROW1";
    INDICES2[INDICES2["COL0ROW2"] = 2] = "COL0ROW2";
    INDICES2[INDICES2["COL0ROW3"] = 3] = "COL0ROW3";
    INDICES2[INDICES2["COL1ROW0"] = 4] = "COL1ROW0";
    INDICES2[INDICES2["COL1ROW1"] = 5] = "COL1ROW1";
    INDICES2[INDICES2["COL1ROW2"] = 6] = "COL1ROW2";
    INDICES2[INDICES2["COL1ROW3"] = 7] = "COL1ROW3";
    INDICES2[INDICES2["COL2ROW0"] = 8] = "COL2ROW0";
    INDICES2[INDICES2["COL2ROW1"] = 9] = "COL2ROW1";
    INDICES2[INDICES2["COL2ROW2"] = 10] = "COL2ROW2";
    INDICES2[INDICES2["COL2ROW3"] = 11] = "COL2ROW3";
    INDICES2[INDICES2["COL3ROW0"] = 12] = "COL3ROW0";
    INDICES2[INDICES2["COL3ROW1"] = 13] = "COL3ROW1";
    INDICES2[INDICES2["COL3ROW2"] = 14] = "COL3ROW2";
    INDICES2[INDICES2["COL3ROW3"] = 15] = "COL3ROW3";
  })(INDICES || (INDICES = {}));
  var DEFAULT_FOVY = 45 * Math.PI / 180;
  var DEFAULT_ASPECT = 1;
  var DEFAULT_NEAR = 0.1;
  var DEFAULT_FAR = 500;
  var IDENTITY_MATRIX = Object.freeze([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);
  var Matrix4 = class extends Matrix {
    static get IDENTITY() {
      return getIdentityMatrix();
    }
    static get ZERO() {
      return getZeroMatrix();
    }
    get ELEMENTS() {
      return 16;
    }
    get RANK() {
      return 4;
    }
    get INDICES() {
      return INDICES;
    }
    constructor(array) {
      super(-0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0);
      if (arguments.length === 1 && Array.isArray(array)) {
        this.copy(array);
      } else {
        this.identity();
      }
    }
    copy(array) {
      this[0] = array[0];
      this[1] = array[1];
      this[2] = array[2];
      this[3] = array[3];
      this[4] = array[4];
      this[5] = array[5];
      this[6] = array[6];
      this[7] = array[7];
      this[8] = array[8];
      this[9] = array[9];
      this[10] = array[10];
      this[11] = array[11];
      this[12] = array[12];
      this[13] = array[13];
      this[14] = array[14];
      this[15] = array[15];
      return this.check();
    }
    // eslint-disable-next-line max-params
    set(m00, m10, m20, m30, m01, m11, m21, m31, m02, m12, m22, m32, m03, m13, m23, m33) {
      this[0] = m00;
      this[1] = m10;
      this[2] = m20;
      this[3] = m30;
      this[4] = m01;
      this[5] = m11;
      this[6] = m21;
      this[7] = m31;
      this[8] = m02;
      this[9] = m12;
      this[10] = m22;
      this[11] = m32;
      this[12] = m03;
      this[13] = m13;
      this[14] = m23;
      this[15] = m33;
      return this.check();
    }
    // accepts row major order, stores as column major
    // eslint-disable-next-line max-params
    setRowMajor(m00, m01, m02, m03, m10, m11, m12, m13, m20, m21, m22, m23, m30, m31, m32, m33) {
      this[0] = m00;
      this[1] = m10;
      this[2] = m20;
      this[3] = m30;
      this[4] = m01;
      this[5] = m11;
      this[6] = m21;
      this[7] = m31;
      this[8] = m02;
      this[9] = m12;
      this[10] = m22;
      this[11] = m32;
      this[12] = m03;
      this[13] = m13;
      this[14] = m23;
      this[15] = m33;
      return this.check();
    }
    toRowMajor(result) {
      result[0] = this[0];
      result[1] = this[4];
      result[2] = this[8];
      result[3] = this[12];
      result[4] = this[1];
      result[5] = this[5];
      result[6] = this[9];
      result[7] = this[13];
      result[8] = this[2];
      result[9] = this[6];
      result[10] = this[10];
      result[11] = this[14];
      result[12] = this[3];
      result[13] = this[7];
      result[14] = this[11];
      result[15] = this[15];
      return result;
    }
    // Constructors
    /** Set to identity matrix */
    identity() {
      return this.copy(IDENTITY_MATRIX);
    }
    /**
     *
     * @param object
     * @returns self
     */
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    fromObject(object) {
      return this.check();
    }
    /**
     * Calculates a 4x4 matrix from the given quaternion
     * @param quaternion Quaternion to create matrix from
     * @returns self
     */
    fromQuaternion(quaternion) {
      fromQuat(this, quaternion);
      return this.check();
    }
    /**
     * Generates a frustum matrix with the given bounds
     * @param view.left - Left bound of the frustum
     * @param view.right - Right bound of the frustum
     * @param view.bottom - Bottom bound of the frustum
     * @param view.top - Top bound of the frustum
     * @param view.near - Near bound of the frustum
     * @param view.far - Far bound of the frustum. Can be set to Infinity.
     * @returns self
     */
    frustum(view) {
      const { left, right, bottom, top, near = DEFAULT_NEAR, far = DEFAULT_FAR } = view;
      if (far === Infinity) {
        computeInfinitePerspectiveOffCenter(this, left, right, bottom, top, near);
      } else {
        frustum(this, left, right, bottom, top, near, far);
      }
      return this.check();
    }
    /**
     * Generates a look-at matrix with the given eye position, focal point,
     * and up axis
     * @param view.eye - (vector) Position of the viewer
     * @param view.center - (vector) Point the viewer is looking at
     * @param view.up - (vector) Up axis
     * @returns self
     */
    lookAt(view) {
      const { eye, center = [0, 0, 0], up = [0, 1, 0] } = view;
      lookAt(this, eye, center, up);
      return this.check();
    }
    /**
     * Generates a orthogonal projection matrix with the given bounds
     * from "traditional" view space parameters
     * @param view.left - Left bound of the frustum
     * @param view.right number  Right bound of the frustum
     * @param view.bottom - Bottom bound of the frustum
     * @param view.top number  Top bound of the frustum
     * @param view.near - Near bound of the frustum
     * @param view.far number  Far bound of the frustum
     * @returns self
     */
    ortho(view) {
      const { left, right, bottom, top, near = DEFAULT_NEAR, far = DEFAULT_FAR } = view;
      ortho(this, left, right, bottom, top, near, far);
      return this.check();
    }
    /**
     * Generates an orthogonal projection matrix with the same parameters
     * as a perspective matrix (plus focalDistance)
     * @param view.fovy Vertical field of view in radians
     * @param view.aspect Aspect ratio. Typically viewport width / viewport height
     * @param view.focalDistance Distance in the view frustum used for extent calculations
     * @param view.near Near bound of the frustum
     * @param view.far Far bound of the frustum
     * @returns self
     */
    orthographic(view) {
      const { fovy = DEFAULT_FOVY, aspect = DEFAULT_ASPECT, focalDistance = 1, near = DEFAULT_NEAR, far = DEFAULT_FAR } = view;
      checkRadians(fovy);
      const halfY = fovy / 2;
      const top = focalDistance * Math.tan(halfY);
      const right = top * aspect;
      return this.ortho({
        left: -right,
        right,
        bottom: -top,
        top,
        near,
        far
      });
    }
    /**
     * Generates a perspective projection matrix with the given bounds
     * @param view.fovy Vertical field of view in radians
     * @param view.aspect Aspect ratio. typically viewport width/height
     * @param view.near Near bound of the frustum
     * @param view.far Far bound of the frustum
     * @returns self
     */
    perspective(view) {
      const { fovy = 45 * Math.PI / 180, aspect = 1, near = 0.1, far = 500 } = view;
      checkRadians(fovy);
      perspective(this, fovy, aspect, near, far);
      return this.check();
    }
    // Accessors
    determinant() {
      return determinant(this);
    }
    /**
     * Extracts the non-uniform scale assuming the matrix is an affine transformation.
     * The scales are the "lengths" of the column vectors in the upper-left 3x3 matrix.
     * @param result
     * @returns self
     */
    getScale(result = [-0, -0, -0]) {
      result[0] = Math.sqrt(this[0] * this[0] + this[1] * this[1] + this[2] * this[2]);
      result[1] = Math.sqrt(this[4] * this[4] + this[5] * this[5] + this[6] * this[6]);
      result[2] = Math.sqrt(this[8] * this[8] + this[9] * this[9] + this[10] * this[10]);
      return result;
    }
    /**
     * Gets the translation portion, assuming the matrix is a affine transformation matrix.
     * @param result
     * @returns self
     */
    getTranslation(result = [-0, -0, -0]) {
      result[0] = this[12];
      result[1] = this[13];
      result[2] = this[14];
      return result;
    }
    /**
     * Gets upper left 3x3 pure rotation matrix (non-scaling), assume affine transformation matrix
     * @param result
     * @param scaleResult
     * @returns self
     */
    getRotation(result, scaleResult) {
      result = result || [-0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0];
      scaleResult = scaleResult || [-0, -0, -0];
      const scale2 = this.getScale(scaleResult);
      const inverseScale0 = 1 / scale2[0];
      const inverseScale1 = 1 / scale2[1];
      const inverseScale2 = 1 / scale2[2];
      result[0] = this[0] * inverseScale0;
      result[1] = this[1] * inverseScale1;
      result[2] = this[2] * inverseScale2;
      result[3] = 0;
      result[4] = this[4] * inverseScale0;
      result[5] = this[5] * inverseScale1;
      result[6] = this[6] * inverseScale2;
      result[7] = 0;
      result[8] = this[8] * inverseScale0;
      result[9] = this[9] * inverseScale1;
      result[10] = this[10] * inverseScale2;
      result[11] = 0;
      result[12] = 0;
      result[13] = 0;
      result[14] = 0;
      result[15] = 1;
      return result;
    }
    /**
     *
     * @param result
     * @param scaleResult
     * @returns self
     */
    getRotationMatrix3(result, scaleResult) {
      result = result || [-0, -0, -0, -0, -0, -0, -0, -0, -0];
      scaleResult = scaleResult || [-0, -0, -0];
      const scale2 = this.getScale(scaleResult);
      const inverseScale0 = 1 / scale2[0];
      const inverseScale1 = 1 / scale2[1];
      const inverseScale2 = 1 / scale2[2];
      result[0] = this[0] * inverseScale0;
      result[1] = this[1] * inverseScale1;
      result[2] = this[2] * inverseScale2;
      result[3] = this[4] * inverseScale0;
      result[4] = this[5] * inverseScale1;
      result[5] = this[6] * inverseScale2;
      result[6] = this[8] * inverseScale0;
      result[7] = this[9] * inverseScale1;
      result[8] = this[10] * inverseScale2;
      return result;
    }
    // Modifiers
    transpose() {
      transpose(this, this);
      return this.check();
    }
    invert() {
      invert(this, this);
      return this.check();
    }
    // Operations
    multiplyLeft(a) {
      multiply(this, a, this);
      return this.check();
    }
    multiplyRight(a) {
      multiply(this, this, a);
      return this.check();
    }
    // Rotates a matrix by the given angle around the X axis
    rotateX(radians) {
      rotateX2(this, this, radians);
      return this.check();
    }
    // Rotates a matrix by the given angle around the Y axis.
    rotateY(radians) {
      rotateY2(this, this, radians);
      return this.check();
    }
    /**
     * Rotates a matrix by the given angle around the Z axis.
     * @param radians
     * @returns self
     */
    rotateZ(radians) {
      rotateZ2(this, this, radians);
      return this.check();
    }
    /**
     *
     * @param param0
     * @returns self
     */
    rotateXYZ(angleXYZ) {
      return this.rotateX(angleXYZ[0]).rotateY(angleXYZ[1]).rotateZ(angleXYZ[2]);
    }
    /**
     *
     * @param radians
     * @param axis
     * @returns self
     */
    rotateAxis(radians, axis) {
      rotate(this, this, radians, axis);
      return this.check();
    }
    /**
     *
     * @param factor
     * @returns self
     */
    scale(factor) {
      scale(this, this, Array.isArray(factor) ? factor : [factor, factor, factor]);
      return this.check();
    }
    /**
     *
     * @param vec
     * @returns self
     */
    translate(vector) {
      translate(this, this, vector);
      return this.check();
    }
    // Transforms
    /**
     * Transforms any 2, 3 or 4 element vector. 2 and 3 elements are treated as points
     * @param vector
     * @param result
     * @returns self
     */
    transform(vector, result) {
      if (vector.length === 4) {
        result = transformMat43(result || [-0, -0, -0, -0], vector, this);
        checkVector(result, 4);
        return result;
      }
      return this.transformAsPoint(vector, result);
    }
    /**
     * Transforms any 2 or 3 element array as point (w implicitly 1)
     * @param vector
     * @param result
     * @returns self
     */
    transformAsPoint(vector, result) {
      const { length } = vector;
      let out;
      switch (length) {
        case 2:
          out = transformMat4(result || [-0, -0], vector, this);
          break;
        case 3:
          out = transformMat42(result || [-0, -0, -0], vector, this);
          break;
        default:
          throw new Error("Illegal vector");
      }
      checkVector(out, vector.length);
      return out;
    }
    /**
     * Transforms any 2 or 3 element array as vector (w implicitly 0)
     * @param vector
     * @param result
     * @returns self
     */
    transformAsVector(vector, result) {
      let out;
      switch (vector.length) {
        case 2:
          out = vec2_transformMat4AsVector(result || [-0, -0], vector, this);
          break;
        case 3:
          out = vec3_transformMat4AsVector(result || [-0, -0, -0], vector, this);
          break;
        default:
          throw new Error("Illegal vector");
      }
      checkVector(out, vector.length);
      return out;
    }
    /** @deprecated */
    transformPoint(vector, result) {
      return this.transformAsPoint(vector, result);
    }
    /** @deprecated */
    transformVector(vector, result) {
      return this.transformAsPoint(vector, result);
    }
    /** @deprecated */
    transformDirection(vector, result) {
      return this.transformAsVector(vector, result);
    }
    // three.js math API compatibility
    makeRotationX(radians) {
      return this.identity().rotateX(radians);
    }
    makeTranslation(x, y, z) {
      return this.identity().translate([x, y, z]);
    }
  };
  var ZERO2;
  var IDENTITY;
  function getZeroMatrix() {
    if (!ZERO2) {
      ZERO2 = new Matrix4([0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
      Object.freeze(ZERO2);
    }
    return ZERO2;
  }
  function getIdentityMatrix() {
    if (!IDENTITY) {
      IDENTITY = new Matrix4();
      Object.freeze(IDENTITY);
    }
    return IDENTITY;
  }
  function checkRadians(possiblyDegrees) {
    if (possiblyDegrees > Math.PI * 2) {
      throw Error("expected radians");
    }
  }
  function computeInfinitePerspectiveOffCenter(result, left, right, bottom, top, near) {
    const column0Row0 = 2 * near / (right - left);
    const column1Row1 = 2 * near / (top - bottom);
    const column2Row0 = (right + left) / (right - left);
    const column2Row1 = (top + bottom) / (top - bottom);
    const column2Row2 = -1;
    const column2Row3 = -1;
    const column3Row2 = -2 * near;
    result[0] = column0Row0;
    result[1] = 0;
    result[2] = 0;
    result[3] = 0;
    result[4] = 0;
    result[5] = column1Row1;
    result[6] = 0;
    result[7] = 0;
    result[8] = column2Row0;
    result[9] = column2Row1;
    result[10] = column2Row2;
    result[11] = column2Row3;
    result[12] = 0;
    result[13] = 0;
    result[14] = column3Row2;
    result[15] = 0;
    return result;
  }

  // src/scenegraph/scenegraph-node.ts
  var ScenegraphNode = class {
    id;
    matrix = new Matrix4();
    display = true;
    position = new Vector3();
    rotation = new Vector3();
    scale = new Vector3(1, 1, 1);
    userData = {};
    props = {};
    constructor(props = {}) {
      const { id } = props;
      this.id = id || uid(this.constructor.name);
      this._setScenegraphNodeProps(props);
    }
    getBounds() {
      return null;
    }
    destroy() {
    }
    /** @deprecated use .destroy() */
    delete() {
      this.destroy();
    }
    setProps(props) {
      this._setScenegraphNodeProps(props);
      return this;
    }
    toString() {
      return `{type: ScenegraphNode, id: ${this.id})}`;
    }
    setPosition(position) {
      this.position = position;
      return this;
    }
    setRotation(rotation) {
      this.rotation = rotation;
      return this;
    }
    setScale(scale2) {
      this.scale = scale2;
      return this;
    }
    setMatrix(matrix, copyMatrix = true) {
      if (copyMatrix) {
        this.matrix.copy(matrix);
      } else {
        this.matrix = matrix;
      }
    }
    setMatrixComponents(components) {
      const { position, rotation, scale: scale2, update = true } = components;
      if (position) {
        this.setPosition(position);
      }
      if (rotation) {
        this.setRotation(rotation);
      }
      if (scale2) {
        this.setScale(scale2);
      }
      if (update) {
        this.updateMatrix();
      }
      return this;
    }
    updateMatrix() {
      const pos = this.position;
      const rot = this.rotation;
      const scale2 = this.scale;
      this.matrix.identity();
      this.matrix.translate(pos);
      this.matrix.rotateXYZ(rot);
      this.matrix.scale(scale2);
      return this;
    }
    update(options = {}) {
      const { position, rotation, scale: scale2 } = options;
      if (position) {
        this.setPosition(position);
      }
      if (rotation) {
        this.setRotation(rotation);
      }
      if (scale2) {
        this.setScale(scale2);
      }
      this.updateMatrix();
      return this;
    }
    getCoordinateUniforms(viewMatrix, modelMatrix) {
      modelMatrix = modelMatrix || this.matrix;
      const worldMatrix = new Matrix4(viewMatrix).multiplyRight(modelMatrix);
      const worldInverse = worldMatrix.invert();
      const worldInverseTranspose = worldInverse.transpose();
      return {
        viewMatrix,
        modelMatrix,
        objectMatrix: modelMatrix,
        worldMatrix,
        worldInverseMatrix: worldInverse,
        worldInverseTransposeMatrix: worldInverseTranspose
      };
    }
    // TODO - copied code, not yet vetted
    /*
      transform() {
        if (!this.parent) {
          this.endPosition.set(this.position);
          this.endRotation.set(this.rotation);
          this.endScale.set(this.scale);
        } else {
          const parent = this.parent;
          this.endPosition.set(this.position.add(parent.endPosition));
          this.endRotation.set(this.rotation.add(parent.endRotation));
          this.endScale.set(this.scale.add(parent.endScale));
        }
    
        const ch = this.children;
        for (let i = 0; i < ch.length; ++i) {
          ch[i].transform();
        }
    
        return this;
      }
      */
    _setScenegraphNodeProps(props) {
      if ("position" in props) {
        this.setPosition(props.position);
      }
      if ("rotation" in props) {
        this.setRotation(props.rotation);
      }
      if ("scale" in props) {
        this.setScale(props.scale);
      }
      if ("matrix" in props) {
        this.setMatrix(props.matrix);
      }
      Object.assign(this.props, props);
    }
  };

  // src/scenegraph/group-node.ts
  var import_core13 = __toESM(require_core(), 1);
  var GroupNode = class extends ScenegraphNode {
    children;
    constructor(props = {}) {
      props = Array.isArray(props) ? { children: props } : props;
      const { children = [] } = props;
      import_core13.log.assert(
        children.every((child) => child instanceof ScenegraphNode),
        "every child must an instance of ScenegraphNode"
      );
      super(props);
      this.children = children;
    }
    getBounds() {
      const result = [
        [Infinity, Infinity, Infinity],
        [-Infinity, -Infinity, -Infinity]
      ];
      this.traverse((node, { worldMatrix }) => {
        const bounds = node.getBounds();
        if (!bounds) {
          return;
        }
        const [min, max] = bounds;
        const center = new Vector3(min).add(max).divide([2, 2, 2]);
        worldMatrix.transformAsPoint(center, center);
        const halfSize = new Vector3(max).subtract(min).divide([2, 2, 2]);
        worldMatrix.transformAsVector(halfSize, halfSize);
        for (let v = 0; v < 8; v++) {
          const position = new Vector3(v & 1 ? -1 : 1, v & 2 ? -1 : 1, v & 4 ? -1 : 1).multiply(halfSize).add(center);
          for (let i = 0; i < 3; i++) {
            result[0][i] = Math.min(result[0][i], position[i]);
            result[1][i] = Math.max(result[1][i], position[i]);
          }
        }
      });
      if (!Number.isFinite(result[0][0])) {
        return null;
      }
      return result;
    }
    destroy() {
      this.children.forEach((child) => child.destroy());
      this.removeAll();
      super.destroy();
    }
    // Unpacks arrays and nested arrays of children
    add(...children) {
      for (const child of children) {
        if (Array.isArray(child)) {
          this.add(...child);
        } else {
          this.children.push(child);
        }
      }
      return this;
    }
    remove(child) {
      const children = this.children;
      const indexOf = children.indexOf(child);
      if (indexOf > -1) {
        children.splice(indexOf, 1);
      }
      return this;
    }
    removeAll() {
      this.children = [];
      return this;
    }
    traverse(visitor, { worldMatrix = new Matrix4() } = {}) {
      const modelMatrix = new Matrix4(worldMatrix).multiplyRight(this.matrix);
      for (const child of this.children) {
        if (child instanceof GroupNode) {
          child.traverse(visitor, { worldMatrix: modelMatrix });
        } else {
          visitor(child, { worldMatrix: modelMatrix });
        }
      }
    }
  };

  // src/scenegraph/model-node.ts
  var ModelNode = class extends ScenegraphNode {
    model;
    bounds = null;
    managedResources;
    // TODO - is this used? override callbacks to make sure we call them with this
    // onBeforeRender = null;
    // onAfterRender = null;
    // AfterRender = null;
    constructor(props) {
      super(props);
      this.model = props.model;
      this.managedResources = props.managedResources || [];
      this.bounds = props.bounds || null;
      this.setProps(props);
    }
    destroy() {
      if (this.model) {
        this.model.destroy();
        this.model = null;
      }
      this.managedResources.forEach((resource) => resource.destroy());
      this.managedResources = [];
    }
    getBounds() {
      return this.bounds;
    }
    // Expose model methods
    draw(renderPass) {
      return this.model.draw(renderPass);
    }
  };

  // src/geometries/truncated-cone-geometry.ts
  var INDEX_OFFSETS = {
    x: [2, 0, 1],
    y: [0, 1, 2],
    z: [1, 2, 0]
  };
  var TruncatedConeGeometry = class extends Geometry {
    constructor(props = {}) {
      const { id = uid("truncated-code-geometry") } = props;
      const { indices, attributes } = tesselateTruncatedCone(props);
      super({
        ...props,
        id,
        topology: "triangle-list",
        indices,
        attributes: {
          POSITION: { size: 3, value: attributes.POSITION },
          NORMAL: { size: 3, value: attributes.NORMAL },
          TEXCOORD_0: { size: 2, value: attributes.TEXCOORD_0 },
          ...props.attributes
        }
      });
    }
  };
  function tesselateTruncatedCone(props = {}) {
    const {
      bottomRadius = 0,
      topRadius = 0,
      height = 1,
      nradial = 10,
      nvertical = 10,
      verticalAxis = "y",
      topCap = false,
      bottomCap = false
    } = props;
    const extra = (topCap ? 2 : 0) + (bottomCap ? 2 : 0);
    const numVertices = (nradial + 1) * (nvertical + 1 + extra);
    const slant = Math.atan2(bottomRadius - topRadius, height);
    const msin = Math.sin;
    const mcos = Math.cos;
    const mpi = Math.PI;
    const cosSlant = mcos(slant);
    const sinSlant = msin(slant);
    const start = topCap ? -2 : 0;
    const end = nvertical + (bottomCap ? 2 : 0);
    const vertsAroundEdge = nradial + 1;
    const indices = new Uint16Array(nradial * (nvertical + extra) * 6);
    const indexOffset = INDEX_OFFSETS[verticalAxis];
    const positions = new Float32Array(numVertices * 3);
    const normals = new Float32Array(numVertices * 3);
    const texCoords = new Float32Array(numVertices * 2);
    let i3 = 0;
    let i2 = 0;
    for (let i = start; i <= end; i++) {
      let v = i / nvertical;
      let y = height * v;
      let ringRadius;
      if (i < 0) {
        y = 0;
        v = 1;
        ringRadius = bottomRadius;
      } else if (i > nvertical) {
        y = height;
        v = 1;
        ringRadius = topRadius;
      } else {
        ringRadius = bottomRadius + (topRadius - bottomRadius) * (i / nvertical);
      }
      if (i === -2 || i === nvertical + 2) {
        ringRadius = 0;
        v = 0;
      }
      y -= height / 2;
      for (let j = 0; j < vertsAroundEdge; j++) {
        const sin = msin(j * mpi * 2 / nradial);
        const cos = mcos(j * mpi * 2 / nradial);
        positions[i3 + indexOffset[0]] = sin * ringRadius;
        positions[i3 + indexOffset[1]] = y;
        positions[i3 + indexOffset[2]] = cos * ringRadius;
        normals[i3 + indexOffset[0]] = i < 0 || i > nvertical ? 0 : sin * cosSlant;
        normals[i3 + indexOffset[1]] = i < 0 ? -1 : i > nvertical ? 1 : sinSlant;
        normals[i3 + indexOffset[2]] = i < 0 || i > nvertical ? 0 : cos * cosSlant;
        texCoords[i2 + 0] = j / nradial;
        texCoords[i2 + 1] = v;
        i2 += 2;
        i3 += 3;
      }
    }
    for (let i = 0; i < nvertical + extra; i++) {
      for (let j = 0; j < nradial; j++) {
        const index = (i * nradial + j) * 6;
        indices[index + 0] = vertsAroundEdge * (i + 0) + 0 + j;
        indices[index + 1] = vertsAroundEdge * (i + 0) + 1 + j;
        indices[index + 2] = vertsAroundEdge * (i + 1) + 1 + j;
        indices[index + 3] = vertsAroundEdge * (i + 0) + 0 + j;
        indices[index + 4] = vertsAroundEdge * (i + 1) + 1 + j;
        indices[index + 5] = vertsAroundEdge * (i + 1) + 0 + j;
      }
    }
    return {
      indices,
      attributes: {
        POSITION: positions,
        NORMAL: normals,
        TEXCOORD_0: texCoords
      }
    };
  }

  // src/geometries/cone-geometry.ts
  var ConeGeometry = class extends TruncatedConeGeometry {
    constructor(props = {}) {
      const { id = uid("cone-geometry"), radius = 1, cap = true } = props;
      super({
        ...props,
        id,
        topRadius: 0,
        topCap: Boolean(cap),
        bottomCap: Boolean(cap),
        bottomRadius: radius
      });
    }
  };

  // src/geometries/cube-geometry.ts
  var CubeGeometry = class extends Geometry {
    constructor(props = {}) {
      const { id = uid("cube-geometry"), indices = true } = props;
      super(
        indices ? {
          ...props,
          id,
          topology: "triangle-list",
          indices: { size: 1, value: CUBE_INDICES },
          attributes: { ...ATTRIBUTES, ...props.attributes }
        } : {
          ...props,
          id,
          topology: "triangle-list",
          indices: void 0,
          attributes: { ...NON_INDEXED_ATTRIBUTES, ...props.attributes }
        }
      );
    }
  };
  var CUBE_INDICES = new Uint16Array([
    0,
    1,
    2,
    0,
    2,
    3,
    4,
    5,
    6,
    4,
    6,
    7,
    8,
    9,
    10,
    8,
    10,
    11,
    12,
    13,
    14,
    12,
    14,
    15,
    16,
    17,
    18,
    16,
    18,
    19,
    20,
    21,
    22,
    20,
    22,
    23
  ]);
  var CUBE_POSITIONS = new Float32Array([
    -1,
    -1,
    1,
    1,
    -1,
    1,
    1,
    1,
    1,
    -1,
    1,
    1,
    -1,
    -1,
    -1,
    -1,
    1,
    -1,
    1,
    1,
    -1,
    1,
    -1,
    -1,
    -1,
    1,
    -1,
    -1,
    1,
    1,
    1,
    1,
    1,
    1,
    1,
    -1,
    -1,
    -1,
    -1,
    1,
    -1,
    -1,
    1,
    -1,
    1,
    -1,
    -1,
    1,
    1,
    -1,
    -1,
    1,
    1,
    -1,
    1,
    1,
    1,
    1,
    -1,
    1,
    -1,
    -1,
    -1,
    -1,
    -1,
    1,
    -1,
    1,
    1,
    -1,
    1,
    -1
  ]);
  var CUBE_NORMALS = new Float32Array([
    // Front face
    0,
    0,
    1,
    0,
    0,
    1,
    0,
    0,
    1,
    0,
    0,
    1,
    // Back face
    0,
    0,
    -1,
    0,
    0,
    -1,
    0,
    0,
    -1,
    0,
    0,
    -1,
    // Top face
    0,
    1,
    0,
    0,
    1,
    0,
    0,
    1,
    0,
    0,
    1,
    0,
    // Bottom face
    0,
    -1,
    0,
    0,
    -1,
    0,
    0,
    -1,
    0,
    0,
    -1,
    0,
    // Right face
    1,
    0,
    0,
    1,
    0,
    0,
    1,
    0,
    0,
    1,
    0,
    0,
    // Left face
    -1,
    0,
    0,
    -1,
    0,
    0,
    -1,
    0,
    0,
    -1,
    0,
    0
  ]);
  var CUBE_TEX_COORDS = new Float32Array([
    // Front face
    0,
    0,
    1,
    0,
    1,
    1,
    0,
    1,
    // Back face
    1,
    0,
    1,
    1,
    0,
    1,
    0,
    0,
    // Top face
    0,
    1,
    0,
    0,
    1,
    0,
    1,
    1,
    // Bottom face
    1,
    1,
    0,
    1,
    0,
    0,
    1,
    0,
    // Right face
    1,
    0,
    1,
    1,
    0,
    1,
    0,
    0,
    // Left face
    0,
    0,
    1,
    0,
    1,
    1,
    0,
    1
  ]);
  var CUBE_NON_INDEXED_POSITIONS = new Float32Array([
    1,
    -1,
    1,
    -1,
    -1,
    1,
    -1,
    -1,
    -1,
    1,
    -1,
    -1,
    1,
    -1,
    1,
    -1,
    -1,
    -1,
    1,
    1,
    1,
    1,
    -1,
    1,
    1,
    -1,
    -1,
    1,
    1,
    -1,
    1,
    1,
    1,
    1,
    -1,
    -1,
    -1,
    1,
    1,
    1,
    1,
    1,
    1,
    1,
    -1,
    -1,
    1,
    -1,
    -1,
    1,
    1,
    1,
    1,
    -1,
    -1,
    -1,
    1,
    -1,
    1,
    1,
    -1,
    1,
    -1,
    -1,
    -1,
    -1,
    -1,
    -1,
    1,
    -1,
    1,
    -1,
    1,
    1,
    1,
    -1,
    1,
    1,
    -1,
    -1,
    1,
    -1,
    -1,
    1,
    1,
    -1,
    1,
    1,
    1,
    1,
    1,
    -1,
    -1,
    -1,
    -1,
    -1,
    -1,
    1,
    -1,
    1,
    1,
    -1,
    1,
    -1,
    -1,
    -1,
    1,
    -1
  ]);
  var CUBE_NON_INDEXED_TEX_COORDS = new Float32Array([
    1,
    1,
    0,
    1,
    0,
    0,
    1,
    0,
    1,
    1,
    0,
    0,
    1,
    1,
    0,
    1,
    0,
    0,
    1,
    0,
    1,
    1,
    0,
    0,
    1,
    1,
    0,
    1,
    0,
    0,
    1,
    0,
    1,
    1,
    0,
    0,
    1,
    1,
    0,
    1,
    0,
    0,
    1,
    0,
    1,
    1,
    0,
    0,
    1,
    1,
    0,
    1,
    0,
    0,
    0,
    0,
    1,
    0,
    1,
    1,
    1,
    1,
    0,
    1,
    0,
    0,
    1,
    0,
    1,
    1,
    0,
    0
  ]);
  var CUBE_NON_INDEXED_COLORS = new Float32Array([
    1,
    0,
    1,
    1,
    0,
    0,
    1,
    1,
    0,
    0,
    0,
    1,
    1,
    0,
    0,
    1,
    1,
    0,
    1,
    1,
    0,
    0,
    0,
    1,
    1,
    1,
    1,
    1,
    1,
    0,
    1,
    1,
    1,
    0,
    0,
    1,
    1,
    1,
    0,
    1,
    1,
    1,
    1,
    1,
    1,
    0,
    0,
    1,
    0,
    1,
    1,
    1,
    1,
    1,
    1,
    1,
    1,
    1,
    0,
    1,
    0,
    1,
    0,
    1,
    0,
    1,
    1,
    1,
    1,
    1,
    0,
    1,
    0,
    0,
    1,
    1,
    0,
    1,
    1,
    1,
    0,
    1,
    0,
    1,
    0,
    0,
    0,
    1,
    0,
    0,
    1,
    1,
    0,
    1,
    0,
    1,
    1,
    1,
    1,
    1,
    0,
    1,
    1,
    1,
    0,
    0,
    1,
    1,
    0,
    0,
    1,
    1,
    1,
    0,
    1,
    1,
    1,
    1,
    1,
    1,
    1,
    0,
    0,
    1,
    0,
    0,
    0,
    1,
    0,
    1,
    0,
    1,
    1,
    1,
    0,
    1,
    1,
    0,
    0,
    1,
    0,
    1,
    0,
    1
  ]);
  var ATTRIBUTES = {
    POSITION: { size: 3, value: CUBE_POSITIONS },
    NORMAL: { size: 3, value: CUBE_NORMALS },
    TEXCOORD_0: { size: 2, value: CUBE_TEX_COORDS }
  };
  var NON_INDEXED_ATTRIBUTES = {
    POSITION: { size: 3, value: CUBE_NON_INDEXED_POSITIONS },
    // NORMAL: {size: 3, value: CUBE_NON_INDEXED_NORMALS},
    TEXCOORD_0: { size: 2, value: CUBE_NON_INDEXED_TEX_COORDS },
    COLOR_0: { size: 3, value: CUBE_NON_INDEXED_COLORS }
  };

  // src/geometries/cylinder-geometry.ts
  var CylinderGeometry = class extends TruncatedConeGeometry {
    constructor(props = {}) {
      const { id = uid("cylinder-geometry"), radius = 1 } = props;
      super({
        ...props,
        id,
        bottomRadius: radius,
        topRadius: radius
      });
    }
  };

  // src/geometries/ico-sphere-geometry.ts
  var ICO_POSITIONS = [-1, 0, 0, 0, 1, 0, 0, 0, -1, 0, 0, 1, 0, -1, 0, 1, 0, 0];
  var ICO_INDICES = [3, 4, 5, 3, 5, 1, 3, 1, 0, 3, 0, 4, 4, 0, 2, 4, 2, 5, 2, 0, 1, 5, 2, 1];
  var IcoSphereGeometry = class extends Geometry {
    constructor(props = {}) {
      const { id = uid("ico-sphere-geometry") } = props;
      const { indices, attributes } = tesselateIcosaHedron(props);
      super({
        ...props,
        id,
        topology: "triangle-list",
        indices,
        attributes: { ...attributes, ...props.attributes }
      });
    }
  };
  function tesselateIcosaHedron(props) {
    const { iterations = 0 } = props;
    const PI = Math.PI;
    const PI2 = PI * 2;
    const positions = [...ICO_POSITIONS];
    let indices = [...ICO_INDICES];
    positions.push();
    indices.push();
    const getMiddlePoint = (() => {
      const pointMemo = {};
      return (i1, i2) => {
        i1 *= 3;
        i2 *= 3;
        const mini = i1 < i2 ? i1 : i2;
        const maxi = i1 > i2 ? i1 : i2;
        const key = `${mini}|${maxi}`;
        if (key in pointMemo) {
          return pointMemo[key];
        }
        const x1 = positions[i1];
        const y1 = positions[i1 + 1];
        const z1 = positions[i1 + 2];
        const x2 = positions[i2];
        const y2 = positions[i2 + 1];
        const z2 = positions[i2 + 2];
        let xm = (x1 + x2) / 2;
        let ym = (y1 + y2) / 2;
        let zm = (z1 + z2) / 2;
        const len = Math.sqrt(xm * xm + ym * ym + zm * zm);
        xm /= len;
        ym /= len;
        zm /= len;
        positions.push(xm, ym, zm);
        return pointMemo[key] = positions.length / 3 - 1;
      };
    })();
    for (let i = 0; i < iterations; i++) {
      const indices2 = [];
      for (let j = 0; j < indices.length; j += 3) {
        const a = getMiddlePoint(indices[j + 0], indices[j + 1]);
        const b = getMiddlePoint(indices[j + 1], indices[j + 2]);
        const c = getMiddlePoint(indices[j + 2], indices[j + 0]);
        indices2.push(c, indices[j + 0], a, a, indices[j + 1], b, b, indices[j + 2], c, a, b, c);
      }
      indices = indices2;
    }
    const normals = new Array(positions.length);
    const texCoords = new Array(positions.length / 3 * 2);
    const l = indices.length;
    for (let i = l - 3; i >= 0; i -= 3) {
      const i1 = indices[i + 0];
      const i2 = indices[i + 1];
      const i3 = indices[i + 2];
      const in1 = i1 * 3;
      const in2 = i2 * 3;
      const in3 = i3 * 3;
      const iu1 = i1 * 2;
      const iu2 = i2 * 2;
      const iu3 = i3 * 2;
      const x1 = positions[in1 + 0];
      const y1 = positions[in1 + 1];
      const z1 = positions[in1 + 2];
      const theta1 = Math.acos(z1 / Math.sqrt(x1 * x1 + y1 * y1 + z1 * z1));
      const phi1 = Math.atan2(y1, x1) + PI;
      const v1 = theta1 / PI;
      const u1 = 1 - phi1 / PI2;
      const x2 = positions[in2 + 0];
      const y2 = positions[in2 + 1];
      const z2 = positions[in2 + 2];
      const theta2 = Math.acos(z2 / Math.sqrt(x2 * x2 + y2 * y2 + z2 * z2));
      const phi2 = Math.atan2(y2, x2) + PI;
      const v2 = theta2 / PI;
      const u2 = 1 - phi2 / PI2;
      const x3 = positions[in3 + 0];
      const y3 = positions[in3 + 1];
      const z3 = positions[in3 + 2];
      const theta3 = Math.acos(z3 / Math.sqrt(x3 * x3 + y3 * y3 + z3 * z3));
      const phi3 = Math.atan2(y3, x3) + PI;
      const v3 = theta3 / PI;
      const u3 = 1 - phi3 / PI2;
      const vec1 = [x3 - x2, y3 - y2, z3 - z2];
      const vec2 = [x1 - x2, y1 - y2, z1 - z2];
      const normal = new Vector3(vec1).cross(vec2).normalize();
      let newIndex;
      if ((u1 === 0 || u2 === 0 || u3 === 0) && (u1 === 0 || u1 > 0.5) && (u2 === 0 || u2 > 0.5) && (u3 === 0 || u3 > 0.5)) {
        positions.push(positions[in1 + 0], positions[in1 + 1], positions[in1 + 2]);
        newIndex = positions.length / 3 - 1;
        indices.push(newIndex);
        texCoords[newIndex * 2 + 0] = 1;
        texCoords[newIndex * 2 + 1] = v1;
        normals[newIndex * 3 + 0] = normal.x;
        normals[newIndex * 3 + 1] = normal.y;
        normals[newIndex * 3 + 2] = normal.z;
        positions.push(positions[in2 + 0], positions[in2 + 1], positions[in2 + 2]);
        newIndex = positions.length / 3 - 1;
        indices.push(newIndex);
        texCoords[newIndex * 2 + 0] = 1;
        texCoords[newIndex * 2 + 1] = v2;
        normals[newIndex * 3 + 0] = normal.x;
        normals[newIndex * 3 + 1] = normal.y;
        normals[newIndex * 3 + 2] = normal.z;
        positions.push(positions[in3 + 0], positions[in3 + 1], positions[in3 + 2]);
        newIndex = positions.length / 3 - 1;
        indices.push(newIndex);
        texCoords[newIndex * 2 + 0] = 1;
        texCoords[newIndex * 2 + 1] = v3;
        normals[newIndex * 3 + 0] = normal.x;
        normals[newIndex * 3 + 1] = normal.y;
        normals[newIndex * 3 + 2] = normal.z;
      }
      normals[in1 + 0] = normals[in2 + 0] = normals[in3 + 0] = normal.x;
      normals[in1 + 1] = normals[in2 + 1] = normals[in3 + 1] = normal.y;
      normals[in1 + 2] = normals[in2 + 2] = normals[in3 + 2] = normal.z;
      texCoords[iu1 + 0] = u1;
      texCoords[iu1 + 1] = v1;
      texCoords[iu2 + 0] = u2;
      texCoords[iu2 + 1] = v2;
      texCoords[iu3 + 0] = u3;
      texCoords[iu3 + 1] = v3;
    }
    return {
      indices: { size: 1, value: new Uint16Array(indices) },
      attributes: {
        POSITION: { size: 3, value: new Float32Array(positions) },
        NORMAL: { size: 3, value: new Float32Array(normals) },
        TEXCOORD_0: { size: 2, value: new Float32Array(texCoords) }
      }
    };
  }

  // src/geometry/geometry-utils.ts
  function unpackIndexedGeometry(geometry) {
    const { indices, attributes } = geometry;
    if (!indices) {
      return geometry;
    }
    const vertexCount = indices.value.length;
    const unpackedAttributes = {};
    for (const attributeName in attributes) {
      const attribute = attributes[attributeName];
      const { constant, value, size } = attribute;
      if (constant || !size) {
        continue;
      }
      const unpackedValue = new value.constructor(vertexCount * size);
      for (let x = 0; x < vertexCount; ++x) {
        const index = indices.value[x];
        for (let i = 0; i < size; i++) {
          unpackedValue[x * size + i] = value[index * size + i];
        }
      }
      unpackedAttributes[attributeName] = { size, value: unpackedValue };
    }
    return {
      attributes: Object.assign({}, attributes, unpackedAttributes)
    };
  }

  // src/geometries/plane-geometry.ts
  var PlaneGeometry = class extends Geometry {
    constructor(props = {}) {
      const { id = uid("plane-geometry") } = props;
      const { indices, attributes } = tesselatePlane(props);
      super({
        ...props,
        id,
        topology: "triangle-list",
        indices,
        attributes: { ...attributes, ...props.attributes }
      });
    }
  };
  function tesselatePlane(props) {
    const { type = "x,y", offset = 0, flipCull = false, unpack = false } = props;
    const coords = type.split(",");
    let c1len = props[`${coords[0]}len`] || 1;
    const c2len = props[`${coords[1]}len`] || 1;
    const subdivisions1 = props[`n${coords[0]}`] || 1;
    const subdivisions2 = props[`n${coords[1]}`] || 1;
    const numVertices = (subdivisions1 + 1) * (subdivisions2 + 1);
    const positions = new Float32Array(numVertices * 3);
    const normals = new Float32Array(numVertices * 3);
    const texCoords = new Float32Array(numVertices * 2);
    if (flipCull) {
      c1len = -c1len;
    }
    let i2 = 0;
    let i3 = 0;
    for (let z = 0; z <= subdivisions2; z++) {
      for (let x = 0; x <= subdivisions1; x++) {
        const u = x / subdivisions1;
        const v = z / subdivisions2;
        texCoords[i2 + 0] = flipCull ? 1 - u : u;
        texCoords[i2 + 1] = v;
        switch (type) {
          case "x,y":
            positions[i3 + 0] = c1len * u - c1len * 0.5;
            positions[i3 + 1] = c2len * v - c2len * 0.5;
            positions[i3 + 2] = offset;
            normals[i3 + 0] = 0;
            normals[i3 + 1] = 0;
            normals[i3 + 2] = flipCull ? 1 : -1;
            break;
          case "x,z":
            positions[i3 + 0] = c1len * u - c1len * 0.5;
            positions[i3 + 1] = offset;
            positions[i3 + 2] = c2len * v - c2len * 0.5;
            normals[i3 + 0] = 0;
            normals[i3 + 1] = flipCull ? 1 : -1;
            normals[i3 + 2] = 0;
            break;
          case "y,z":
            positions[i3 + 0] = offset;
            positions[i3 + 1] = c1len * u - c1len * 0.5;
            positions[i3 + 2] = c2len * v - c2len * 0.5;
            normals[i3 + 0] = flipCull ? 1 : -1;
            normals[i3 + 1] = 0;
            normals[i3 + 2] = 0;
            break;
          default:
            throw new Error("PlaneGeometry: unknown type");
        }
        i2 += 2;
        i3 += 3;
      }
    }
    const numVertsAcross = subdivisions1 + 1;
    const indices = new Uint16Array(subdivisions1 * subdivisions2 * 6);
    for (let z = 0; z < subdivisions2; z++) {
      for (let x = 0; x < subdivisions1; x++) {
        const index = (z * subdivisions1 + x) * 6;
        indices[index + 0] = (z + 0) * numVertsAcross + x;
        indices[index + 1] = (z + 1) * numVertsAcross + x;
        indices[index + 2] = (z + 0) * numVertsAcross + x + 1;
        indices[index + 3] = (z + 1) * numVertsAcross + x;
        indices[index + 4] = (z + 1) * numVertsAcross + x + 1;
        indices[index + 5] = (z + 0) * numVertsAcross + x + 1;
      }
    }
    const geometry = {
      indices: { size: 1, value: indices },
      attributes: {
        POSITION: { size: 3, value: positions },
        NORMAL: { size: 3, value: normals },
        TEXCOORD_0: { size: 2, value: texCoords }
      }
    };
    return unpack ? unpackIndexedGeometry(geometry) : geometry;
  }

  // src/geometries/sphere-geometry.ts
  var SphereGeometry = class extends Geometry {
    constructor(props = {}) {
      const { id = uid("sphere-geometry") } = props;
      const { indices, attributes } = tesselateSphere(props);
      super({
        ...props,
        id,
        topology: "triangle-list",
        indices,
        attributes: { ...attributes, ...props.attributes }
      });
    }
  };
  function tesselateSphere(props) {
    const { nlat = 10, nlong = 10 } = props;
    const startLat = 0;
    const endLat = Math.PI;
    const latRange = endLat - startLat;
    const startLong = 0;
    const endLong = 2 * Math.PI;
    const longRange = endLong - startLong;
    const numVertices = (nlat + 1) * (nlong + 1);
    const radius = (n1, n2, n3, u, v) => props.radius || 1;
    const positions = new Float32Array(numVertices * 3);
    const normals = new Float32Array(numVertices * 3);
    const texCoords = new Float32Array(numVertices * 2);
    const IndexType = numVertices > 65535 ? Uint32Array : Uint16Array;
    const indices = new IndexType(nlat * nlong * 6);
    for (let y = 0; y <= nlat; y++) {
      for (let x = 0; x <= nlong; x++) {
        const u = x / nlong;
        const v = y / nlat;
        const index = x + y * (nlong + 1);
        const i2 = index * 2;
        const i3 = index * 3;
        const theta = longRange * u;
        const phi = latRange * v;
        const sinTheta = Math.sin(theta);
        const cosTheta = Math.cos(theta);
        const sinPhi = Math.sin(phi);
        const cosPhi = Math.cos(phi);
        const ux = cosTheta * sinPhi;
        const uy = cosPhi;
        const uz = sinTheta * sinPhi;
        const r = radius(ux, uy, uz, u, v);
        positions[i3 + 0] = r * ux;
        positions[i3 + 1] = r * uy;
        positions[i3 + 2] = r * uz;
        normals[i3 + 0] = ux;
        normals[i3 + 1] = uy;
        normals[i3 + 2] = uz;
        texCoords[i2 + 0] = u;
        texCoords[i2 + 1] = 1 - v;
      }
    }
    const numVertsAround = nlong + 1;
    for (let x = 0; x < nlong; x++) {
      for (let y = 0; y < nlat; y++) {
        const index = (x * nlat + y) * 6;
        indices[index + 0] = y * numVertsAround + x;
        indices[index + 1] = y * numVertsAround + x + 1;
        indices[index + 2] = (y + 1) * numVertsAround + x;
        indices[index + 3] = (y + 1) * numVertsAround + x;
        indices[index + 4] = y * numVertsAround + x + 1;
        indices[index + 5] = (y + 1) * numVertsAround + x + 1;
      }
    }
    return {
      indices: { size: 1, value: indices },
      attributes: {
        POSITION: { size: 3, value: positions },
        NORMAL: { size: 3, value: normals },
        TEXCOORD_0: { size: 2, value: texCoords }
      }
    };
  }

  // src/application-utils/random.ts
  function makeRandomGenerator() {
    let s = 1;
    let c = 1;
    return () => {
      s = Math.sin(c * 17.23);
      c = Math.cos(s * 27.92);
      return fract(Math.abs(s * c) * 1432.71);
    };
  }
  function fract(n) {
    return n - Math.floor(n);
  }

  // src/passes/shader-pass-renderer.ts
  var import_shadertools5 = __toESM(require_shadertools(), 1);

  // src/compute/swap.ts
  var import_core15 = __toESM(require_core(), 1);
  var Swap = class {
    /** The current resource - usually the source for renders or computations */
    current;
    /** The next resource - usually the target/destination for transforms / computations */
    next;
    constructor(props) {
      this.current = props.current;
      this.next = props.next;
    }
    /** Destroys the two managed resources */
    destroy() {
      this.current?.destroy();
      this.next?.destroy();
    }
    /** Make the next resource into the current resource, and reuse the current resource as the next resource */
    swap() {
      const current = this.current;
      this.current = this.next;
      this.next = current;
    }
  };
  var SwapFramebuffers = class extends Swap {
    constructor(device, props) {
      props = { ...props };
      let colorAttachments = props.colorAttachments?.map(
        (colorAttachment) => typeof colorAttachment !== "string" ? colorAttachment : device.createTexture({
          format: colorAttachment,
          usage: import_core15.Texture.SAMPLE | import_core15.Texture.RENDER | import_core15.Texture.COPY_SRC | import_core15.Texture.COPY_DST,
          width: 1,
          height: 1
        })
      );
      const current = device.createFramebuffer({ ...props, colorAttachments });
      colorAttachments = props.colorAttachments?.map(
        (colorAttachment) => typeof colorAttachment !== "string" ? colorAttachment : device.createTexture({
          format: colorAttachment,
          usage: import_core15.Texture.TEXTURE | import_core15.Texture.COPY_SRC | import_core15.Texture.COPY_DST | import_core15.Texture.RENDER_ATTACHMENT,
          width: 1,
          height: 1
        })
      );
      const next = device.createFramebuffer({ ...props, colorAttachments });
      super({ current, next });
    }
    /**
     * Resizes the Framebuffers.
     * @returns true if the size changed, otherwise exiting framebuffers were preserved
     * @note any contents are not preserved!
     */
    resize(size) {
      if (size.width === this.current.width && size.height === this.current.height) {
        return false;
      }
      const { current, next } = this;
      this.current = current.clone(size);
      current.destroy();
      this.next = next.clone(size);
      next.destroy();
      return true;
    }
  };
  var SwapBuffers = class extends Swap {
    constructor(device, props) {
      super({ current: device.createBuffer(props), next: device.createBuffer(props) });
    }
    /**
     * Resizes the Buffers.
     * @returns true if the size changed, otherwise exiting buffers were preserved.
     * @note any contents are not preserved!
     */
    resize(props) {
      if (props.byteLength === this.current.byteLength) {
        return false;
      }
      const { current, next } = this;
      this.current = current.clone(props);
      current.destroy();
      this.next = next.clone(props);
      next.destroy();
      return true;
    }
  };

  // src/passes/get-fragment-shader.ts
  function getFragmentShaderForRenderPass(options) {
    const { shaderPass, action, shadingLanguage } = options;
    switch (action) {
      case "filter":
        const filterFunc = `${shaderPass.name}_filterColor_ext`;
        return shadingLanguage === "wgsl" ? getFilterShaderWGSL(filterFunc) : getFilterShaderGLSL(filterFunc);
      case "sample":
        const samplerFunc = `${shaderPass.name}_sampleColor`;
        return shadingLanguage === "wgsl" ? getSamplerShaderWGSL(samplerFunc) : getSamplerShaderGLSL(samplerFunc);
      default:
        throw new Error(`${shaderPass.name} no fragment shader generated for shader pass`);
    }
  }
  function getFilterShaderWGSL(func) {
    return (
      /* wgsl */
      `// Binding 0:1 is reserved for shader passes
// @group(0) @binding(0) var<uniform> brightnessContrast : brightnessContrastUniforms;
@group(0) @binding(1) var texture: texture_2d<f32>;
@group(0) @binding(2) var sampler: sampler;

struct FragmentInputs {
  @location(0) fragUV: vec2f,
  @location(1) fragPosition: vec4f,
  @location(2) fragCoordinate: vec4f
};

@fragment
fn fragmentMain(inputs: FragmentInputs) -> @location(0) vec4f {
  let texSize = textureDimensions(texture, 0);
  var fragColor = textureSample(texture, sampler, fragUV);
  fragColor = ${func}(gl_FragColor, texSize, texCoord);
  return fragColor;
}
`
    );
  }
  function getSamplerShaderWGSL(func) {
    return (
      /* wgsl */
      `// Binding 0:1 is reserved for shader passes
@group(0) @binding(0) var<uniform> brightnessContrast : brightnessContrastUniforms;
@group(0) @binding(1) var texture: texture_2d<f32>;
@group(0) @binding(2) var sampler: sampler;

struct FragmentInputs = {
  @location(0) fragUV: vec2f,
  @location(1) fragPosition: vec4f,
  @location(2) fragCoordinate: vec4f
};

@fragment
fn fragmentMain(inputs: FragmentInputs) -> @location(0) vec4f {
  let texSize = textureDimensions(texture, 0);
  var fragColor = textureSample(texture, sampler, fragUV);
  fragColor = ${func}(gl_FragColor, texSize, texCoord);
  return fragColor;
}
`
    );
  }
  function getFilterShaderGLSL(func) {
    return (
      /* glsl */
      `#version 300 es

uniform sampler2D sourceTexture;

in vec2 position;
in vec2 coordinate;
in vec2 uv;

out vec4 fragColor;

void main() {
  vec2 texCoord = coordinate;
  ivec2 iTexSize = textureSize(sourceTexture, 0);
  vec2 texSize = vec2(float(iTexSize.x), float(iTexSize.y));

  fragColor = texture(sourceTexture, texCoord);
  fragColor = ${func}(fragColor, texSize, texCoord);
}
`
    );
  }
  function getSamplerShaderGLSL(func) {
    return (
      /* glsl */
      `#version 300 es

uniform sampler2D sourceTexture;

in vec2 position;
in vec2 coordinate;
in vec2 uv;

out vec4 fragColor;

void main() {
  vec2 texCoord = coordinate;
  ivec2 iTexSize = textureSize(sourceTexture, 0);
  vec2 texSize = vec2(float(iTexSize.x), float(iTexSize.y));

  fragColor = ${func}(sourceTexture, texSize, texCoord);
}
`
    );
  }

  // src/passes/shader-pass-renderer.ts
  var ShaderPassRenderer = class {
    device;
    shaderInputs;
    passRenderers;
    swapFramebuffers;
    /** For rendering to the screen */
    clipSpace;
    textureModel;
    constructor(device, props) {
      this.device = device;
      props.shaderPasses.map((shaderPass) => (0, import_shadertools5.initializeShaderModule)(shaderPass));
      const modules = props.shaderPasses.reduce(
        (object, shaderPass) => ({ ...object, [shaderPass.name]: shaderPass }),
        {}
      );
      this.shaderInputs = props.shaderInputs || new ShaderInputs(modules);
      const size = device.getCanvasContext().getDrawingBufferSize();
      this.swapFramebuffers = new SwapFramebuffers(device, {
        colorAttachments: [device.preferredColorFormat],
        width: size[0],
        height: size[1]
      });
      this.textureModel = new BackgroundTextureModel(device, {
        backgroundTexture: this.swapFramebuffers.current.colorAttachments[0].texture
      });
      this.clipSpace = new ClipSpace(device, {
        source: (
          /* wgsl */
          `  @group(0) @binding(0) var sourceTexture: texture_2d<f32>;
  @group(0) @binding(1) var sourceTextureSampler: sampler;

@fragment
fn fragmentMain(inputs: FragmentInputs) -> @location(0) vec4<f32> {
	let texCoord: vec2<f32> = inputs.coordinate;
	return textureSample(sourceTexture, sourceTextureSampler, texCoord);
}
`
        ),
        fs: (
          /* glsl */
          `#version 300 es

uniform sampler2D sourceTexture;
in vec2 uv;
in vec2 coordinate;
out vec4 fragColor;

void main() {
  vec2 texCoord = coordinate;
  fragColor = texture(sourceTexture, coordinate);
}
`
        )
      });
      this.passRenderers = props.shaderPasses.map((shaderPass) => new PassRenderer(device, shaderPass));
    }
    /** Destroys resources created by this ShaderPassRenderer */
    destroy() {
      for (const subPassRenderer of this.passRenderers) {
        subPassRenderer.destroy();
      }
      this.swapFramebuffers.destroy();
      this.clipSpace.destroy();
    }
    resize(width, height) {
      this.swapFramebuffers.resize({ width, height });
    }
    renderToScreen(options) {
      const outputTexture = this.renderToTexture(options);
      if (!outputTexture) {
        return false;
      }
      const framebuffer = this.device.getDefaultCanvasContext().getCurrentFramebuffer({ depthStencilAttachment: false });
      const renderPass = this.device.beginRenderPass({
        id: "shader-pass-renderer-to-screen",
        framebuffer,
        clearColor: [0, 0, 0, 1],
        clearDepth: 1
      });
      this.clipSpace.setBindings({ sourceTexture: outputTexture });
      this.clipSpace.draw(renderPass);
      renderPass.end();
      return true;
    }
    /** Runs the shaderPasses in sequence on the sourceTexture and returns a texture with the results.
     * @returns null if the the sourceTexture has not yet been loaded
     */
    renderToTexture(options) {
      const { sourceTexture } = options;
      if (!sourceTexture.isReady) {
        return null;
      }
      this.textureModel.destroy();
      this.textureModel = new BackgroundTextureModel(this.device, {
        backgroundTexture: sourceTexture
      });
      const clearTexturePass = this.device.beginRenderPass({
        id: "shader-pass-renderer-clear-texture",
        framebuffer: this.swapFramebuffers.current,
        clearColor: [0, 0, 0, 1]
      });
      this.textureModel.draw(clearTexturePass);
      clearTexturePass.end();
      let first = true;
      for (const passRenderer of this.passRenderers) {
        for (const subPassRenderer of passRenderer.subPassRenderers) {
          if (!first) {
            this.swapFramebuffers.swap();
          }
          first = false;
          const swapBufferTexture = this.swapFramebuffers.current.colorAttachments[0].texture;
          const bindings = {
            sourceTexture: swapBufferTexture
            // texSize: [sourceTextures.width, sourceTextures.height]
          };
          const renderPass = this.device.beginRenderPass({
            id: "shader-pass-renderer-run-pass",
            framebuffer: this.swapFramebuffers.next,
            clearColor: [0, 0, 0, 1],
            clearDepth: 1
          });
          subPassRenderer.render({ renderPass, bindings });
          renderPass.end();
        }
      }
      this.swapFramebuffers.swap();
      const outputTexture = this.swapFramebuffers.current.colorAttachments[0].texture;
      return outputTexture;
    }
  };
  var PassRenderer = class {
    shaderPass;
    subPassRenderers;
    constructor(device, shaderPass, props = {}) {
      this.shaderPass = shaderPass;
      const subPasses = shaderPass.passes || [];
      this.subPassRenderers = subPasses.map((subPass) => {
        return new SubPassRenderer(device, shaderPass, subPass);
      });
    }
    destroy() {
      for (const subPassRenderer of this.subPassRenderers) {
        subPassRenderer.destroy();
      }
    }
  };
  var SubPassRenderer = class {
    model;
    shaderPass;
    subPass;
    constructor(device, shaderPass, subPass) {
      this.shaderPass = shaderPass;
      this.subPass = subPass;
      const action = subPass.action || subPass.filter && "filter" || subPass.sampler && "sample" || "filter";
      const fs3 = getFragmentShaderForRenderPass({
        shaderPass,
        action,
        shadingLanguage: device.info.shadingLanguage
      });
      this.model = new ClipSpace(device, {
        id: `${shaderPass.name}-subpass`,
        source: fs3,
        fs: fs3,
        modules: [shaderPass],
        parameters: {
          depthWriteEnabled: false
        }
      });
    }
    destroy() {
      this.model.destroy();
    }
    render(options) {
      const { renderPass, bindings } = options;
      this.model.shaderInputs.setProps({
        [this.shaderPass.name]: this.shaderPass.uniforms || {}
      });
      this.model.shaderInputs.setProps({
        [this.shaderPass.name]: this.subPass.uniforms || {}
      });
      this.model.setBindings(bindings || {});
      this.model.draw(renderPass);
    }
  };

  // src/compute/computation.ts
  var import_core16 = __toESM(require_core(), 1);
  var import_shadertools6 = __toESM(require_shadertools(), 1);
  var LOG_DRAW_PRIORITY2 = 2;
  var LOG_DRAW_TIMEOUT2 = 1e4;
  var _Computation = class {
    device;
    id;
    pipelineFactory;
    shaderFactory;
    userData = {};
    /** Bindings (textures, samplers, uniform buffers) */
    bindings = {};
    /** The underlying GPU pipeline. */
    pipeline;
    /** Assembled compute shader source */
    source;
    /** the underlying compiled compute shader */
    // @ts-ignore Set in function called from constructor
    shader;
    /** ShaderInputs instance */
    shaderInputs;
    // @ts-ignore Set in function called from constructor
    _uniformStore;
    _pipelineNeedsUpdate = "newly created";
    _getModuleUniforms;
    props;
    _destroyed = false;
    constructor(device, props) {
      if (device.type !== "webgpu") {
        throw new Error("Computation is only supported in WebGPU");
      }
      this.props = { ..._Computation.defaultProps, ...props };
      props = this.props;
      this.id = props.id || uid("model");
      this.device = device;
      Object.assign(this.userData, props.userData);
      const moduleMap = Object.fromEntries(
        this.props.modules?.map((module) => [module.name, module]) || []
      );
      this.shaderInputs = props.shaderInputs || new ShaderInputs(moduleMap);
      this.setShaderInputs(this.shaderInputs);
      this.props.shaderLayout ||= (0, import_shadertools6.getShaderLayoutFromWGSL)(this.props.source);
      const platformInfo = getPlatformInfo2(device);
      const modules = (this.props.modules?.length > 0 ? this.props.modules : this.shaderInputs?.getModules()) || [];
      this.pipelineFactory = props.pipelineFactory || PipelineFactory.getDefaultPipelineFactory(this.device);
      this.shaderFactory = props.shaderFactory || ShaderFactory.getDefaultShaderFactory(this.device);
      const { source: source3, getUniforms: getUniforms2 } = this.props.shaderAssembler.assembleWGSLShader({
        platformInfo,
        ...this.props,
        modules
      });
      this.source = source3;
      this._getModuleUniforms = getUniforms2;
      this.pipeline = this._updatePipeline();
      if (props.bindings) {
        this.setBindings(props.bindings);
      }
      Object.seal(this);
    }
    destroy() {
      if (this._destroyed)
        return;
      this.pipelineFactory.release(this.pipeline);
      this.shaderFactory.release(this.shader);
      this._uniformStore.destroy();
      this._destroyed = true;
    }
    // Draw call
    predraw() {
      this.updateShaderInputs();
    }
    dispatch(computePass, x, y, z) {
      try {
        this._logDrawCallStart();
        this.pipeline = this._updatePipeline();
        this.pipeline.setBindings(this.bindings);
        computePass.setPipeline(this.pipeline);
        computePass.setBindings([]);
        computePass.dispatch(x, y, z);
      } finally {
        this._logDrawCallEnd();
      }
    }
    // Update fixed fields (can trigger pipeline rebuild)
    // Update dynamic fields
    /**
     * Updates the vertex count (used in draw calls)
     * @note Any attributes with stepMode=vertex need to be at least this big
     */
    setVertexCount(vertexCount) {
    }
    /**
     * Updates the instance count (used in draw calls)
     * @note Any attributes with stepMode=instance need to be at least this big
     */
    setInstanceCount(instanceCount) {
    }
    setShaderInputs(shaderInputs) {
      this.shaderInputs = shaderInputs;
      this._uniformStore = new import_core16.UniformStore(this.shaderInputs.modules);
      for (const moduleName of Object.keys(this.shaderInputs.modules)) {
        const uniformBuffer = this._uniformStore.getManagedUniformBuffer(this.device, moduleName);
        this.bindings[`${moduleName}Uniforms`] = uniformBuffer;
      }
    }
    /**
     * Updates shader module settings (which results in uniforms being set)
     */
    setShaderModuleProps(props) {
      const uniforms = this._getModuleUniforms(props);
      const keys = Object.keys(uniforms).filter((k) => {
        const uniform = uniforms[k];
        return !isNumericArray(uniform) && typeof uniform !== "number" && typeof uniform !== "boolean";
      });
      const bindings = {};
      for (const k of keys) {
        bindings[k] = uniforms[k];
        delete uniforms[k];
      }
    }
    updateShaderInputs() {
      this._uniformStore.setUniforms(this.shaderInputs.getUniformValues());
    }
    /**
     * Sets bindings (textures, samplers, uniform buffers)
     */
    setBindings(bindings) {
      Object.assign(this.bindings, bindings);
    }
    _setPipelineNeedsUpdate(reason) {
      this._pipelineNeedsUpdate = this._pipelineNeedsUpdate || reason;
    }
    _updatePipeline() {
      if (this._pipelineNeedsUpdate) {
        let prevShader = null;
        if (this.pipeline) {
          import_core16.log.log(
            1,
            `Model ${this.id}: Recreating pipeline because "${this._pipelineNeedsUpdate}".`
          )();
          prevShader = this.shader;
        }
        this._pipelineNeedsUpdate = false;
        this.shader = this.shaderFactory.createShader({
          id: `${this.id}-fragment`,
          stage: "compute",
          source: this.source,
          debugShaders: this.props.debugShaders
        });
        this.pipeline = this.pipelineFactory.createComputePipeline({
          ...this.props,
          shader: this.shader
        });
        if (prevShader) {
          this.shaderFactory.release(prevShader);
        }
      }
      return this.pipeline;
    }
    /** Throttle draw call logging */
    _lastLogTime = 0;
    _logOpen = false;
    _logDrawCallStart() {
      const logDrawTimeout = import_core16.log.level > 3 ? 0 : LOG_DRAW_TIMEOUT2;
      if (import_core16.log.level < 2 || Date.now() - this._lastLogTime < logDrawTimeout) {
        return;
      }
      this._lastLogTime = Date.now();
      this._logOpen = true;
      import_core16.log.group(LOG_DRAW_PRIORITY2, `>>> DRAWING MODEL ${this.id}`, { collapsed: import_core16.log.level <= 2 })();
    }
    _logDrawCallEnd() {
      if (this._logOpen) {
        const uniformTable = this.shaderInputs.getDebugTable();
        import_core16.log.table(LOG_DRAW_PRIORITY2, uniformTable)();
        import_core16.log.groupEnd(LOG_DRAW_PRIORITY2)();
        this._logOpen = false;
      }
    }
    _drawCount = 0;
    // TODO - fix typing of luma data types
    _getBufferOrConstantValues(attribute, dataType) {
      const TypedArrayConstructor = (0, import_core16.getTypedArrayConstructor)(dataType);
      const typedArray = attribute instanceof import_core16.Buffer ? new TypedArrayConstructor(attribute.debugData) : attribute;
      return typedArray.toString();
    }
  };
  var Computation = _Computation;
  __publicField(Computation, "defaultProps", {
    ...import_core16.ComputePipeline.defaultProps,
    id: "unnamed",
    handle: void 0,
    userData: {},
    source: "",
    modules: [],
    defines: {},
    bindings: void 0,
    shaderInputs: void 0,
    pipelineFactory: void 0,
    shaderFactory: void 0,
    shaderAssembler: import_shadertools6.ShaderAssembler.getDefaultShaderAssembler(),
    debugShaders: void 0
  });
  function getPlatformInfo2(device) {
    return {
      type: device.type,
      shaderLanguage: device.info.shadingLanguage,
      shaderLanguageVersion: device.info.shadingLanguageVersion,
      gpu: device.info.gpu,
      // HACK - we pretend that the DeviceFeatures is a Set, it has a similar API
      features: device.features
    };
  }

  // src/modules/picking/picking-uniforms.ts
  var DEFAULT_HIGHLIGHT_COLOR = [0, 1, 1, 1];
  var INVALID_INDEX = -1;
  var uniformTypes = {
    isActive: "i32",
    indexMode: "i32",
    batchIndex: "i32",
    isHighlightActive: "i32",
    highlightedBatchIndex: "i32",
    highlightedObjectIndex: "i32",
    highlightColor: "vec4<f32>"
  };
  var GLSL_UNIFORMS = (
    /* glsl */
    `precision highp float;
precision highp int;

uniform pickingUniforms {
  int isActive;
  int indexMode;
  int batchIndex;

  int isHighlightActive;
  int highlightedBatchIndex;
  int highlightedObjectIndex;
  vec4 highlightColor;
} picking;
`
  );
  var WGSL_UNIFORMS = (
    /* wgsl */
    `struct pickingUniforms {
  isActive: int32;
  indexMode: int32;
  batchIndex: int32;

  isHighlightActive: int32;
  highlightedBatchIndex: int32;
  highlightedObjectIndex: int32;
  highlightColor: vec4<f32>;
} picking;
`
  );
  function getUniforms(props = {}, prevUniforms) {
    const uniforms = { ...prevUniforms };
    if (props.isActive !== void 0) {
      uniforms.isActive = Boolean(props.isActive);
    }
    switch (props.indexMode) {
      case "instance":
        uniforms.indexMode = 0;
        break;
      case "custom":
        uniforms.indexMode = 1;
        break;
      case void 0:
        break;
    }
    switch (props.highlightedObjectIndex) {
      case void 0:
        break;
      case null:
        uniforms.isHighlightActive = false;
        uniforms.highlightedObjectIndex = INVALID_INDEX;
        break;
      default:
        uniforms.isHighlightActive = true;
        uniforms.highlightedObjectIndex = props.highlightedObjectIndex;
    }
    if (typeof props.highlightedBatchIndex === "number") {
      uniforms.highlightedBatchIndex = props.highlightedBatchIndex;
    }
    if (props.highlightColor) {
      uniforms.highlightColor = props.highlightColor;
    }
    return uniforms;
  }
  var pickingUniforms = {
    props: {},
    uniforms: {},
    name: "picking",
    uniformTypes,
    defaultUniforms: {
      isActive: false,
      indexMode: 0,
      batchIndex: 0,
      isHighlightActive: true,
      highlightedBatchIndex: INVALID_INDEX,
      highlightedObjectIndex: INVALID_INDEX,
      highlightColor: DEFAULT_HIGHLIGHT_COLOR
    },
    getUniforms
  };

  // src/modules/picking/picking-manager.ts
  var _PickingManager = class {
    device;
    props;
    /** Info from latest pick operation */
    pickInfo = { batchIndex: null, objectIndex: null };
    /** Framebuffer used for picking */
    framebuffer = null;
    constructor(device, props) {
      this.device = device;
      this.props = { ..._PickingManager.defaultProps, ...props };
    }
    destroy() {
      this.framebuffer?.destroy();
    }
    // TODO - Ask for a cached framebuffer? a Framebuffer factory?
    getFramebuffer() {
      if (!this.framebuffer) {
        this.framebuffer = this.device.createFramebuffer({
          colorAttachments: ["rgba8unorm", "rg32sint"],
          depthStencilAttachment: "depth24plus"
        });
      }
      return this.framebuffer;
    }
    /** Clear highlighted / picked object */
    clearPickState() {
      this.props.shaderInputs.setProps({ picking: { highlightedObjectIndex: null } });
    }
    /** Prepare for rendering picking colors */
    beginRenderPass() {
      const framebuffer = this.getFramebuffer();
      framebuffer.resize(this.device.getDefaultCanvasContext().getDevicePixelSize());
      this.props.shaderInputs?.setProps({ picking: { isActive: true } });
      const pickingPass = this.device.beginRenderPass({
        framebuffer,
        clearColors: [new Float32Array([0, 0, 0, 0]), new Int32Array([-1, -1, 0, 0])],
        clearDepth: 1
      });
      return pickingPass;
    }
    async updatePickInfo(mousePosition) {
      const framebuffer = this.getFramebuffer();
      const [pickX, pickY] = this.getPickPosition(mousePosition);
      const pixelData = this.device.readPixelsToArrayWebGL(framebuffer, {
        sourceX: pickX,
        sourceY: pickY,
        sourceWidth: 1,
        sourceHeight: 1,
        sourceAttachment: 1
      });
      if (!pixelData) {
        return null;
      }
      const pickInfo = {
        objectIndex: pixelData[0] === INVALID_INDEX ? null : pixelData[0],
        batchIndex: pixelData[1] === INVALID_INDEX ? null : pixelData[1]
      };
      if (pickInfo.objectIndex !== this.pickInfo.objectIndex || pickInfo.batchIndex !== this.pickInfo.batchIndex) {
        this.pickInfo = pickInfo;
        this.props.onObjectPicked(pickInfo);
      }
      this.props.shaderInputs?.setProps({
        picking: {
          isActive: false,
          highlightedBatchIndex: pickInfo.batchIndex,
          highlightedObjectIndex: pickInfo.objectIndex
        }
      });
      return this.pickInfo;
    }
    /**
     * Get pick position in device pixel range
     * use the center pixel location in device pixel range
     */
    getPickPosition(mousePosition) {
      const devicePixels = this.device.getDefaultCanvasContext().cssToDevicePixels(mousePosition);
      const pickX = devicePixels.x + Math.floor(devicePixels.width / 2);
      const pickY = devicePixels.y + Math.floor(devicePixels.height / 2);
      return [pickX, pickY];
    }
  };
  var PickingManager = _PickingManager;
  __publicField(PickingManager, "defaultProps", {
    shaderInputs: void 0,
    onObjectPicked: () => {
    }
  });

  // src/modules/picking/index-picking.ts
  var source = (
    /* wgsl */
    `${WGSL_UNIFORMS}

const INDEX_PICKING_MODE_INSTANCE = 0;
const INDEX_PICKING_MODE_CUSTOM = 1;
const INDEX_PICKING_INVALID_INDEX = ${INVALID_INDEX}; // 2^32 - 1

struct indexPickingFragmentInputs = {
  objectIndex: int32;
};

let indexPickingFragmentInputs: indexPickingFragmentInputs;

/**
 * Vertex shaders should call this function to set the object index.
 * If using instance or vertex mode, argument will be ignored, 0 can be supplied.
 */
fn picking_setObjectIndex(objectIndex: int32) {
  switch (picking.indexMode) {
    case INDEX_PICKING_MODE_INSTANCE, default: {
      picking_objectIndex = instance_index;
    };
    case INDEX_PICKING_MODE_CUSTOM: {
      picking_objectIndex = objectIndex;
    };
  }
}

`
  );
  var vs = (
    /* glsl */
    `${GLSL_UNIFORMS}

const int INDEX_PICKING_MODE_INSTANCE = 0;
const int INDEX_PICKING_MODE_CUSTOM = 1;

const int INDEX_PICKING_INVALID_INDEX = ${INVALID_INDEX}; // 2^32 - 1

flat out int picking_objectIndex;

/**
 * Vertex shaders should call this function to set the object index.
 * If using instance or vertex mode, argument will be ignored, 0 can be supplied.
 */
void picking_setObjectIndex(int objectIndex) {
  switch (picking.indexMode) {
    case INDEX_PICKING_MODE_INSTANCE:
      picking_objectIndex = gl_InstanceID;
      break;
    case INDEX_PICKING_MODE_CUSTOM:
      picking_objectIndex = objectIndex;
      break;
  }
}
`
  );
  var fs = (
    /* glsl */
    `${GLSL_UNIFORMS}

const int INDEX_PICKING_INVALID_INDEX = ${INVALID_INDEX}; // 2^32 - 1

flat in int picking_objectIndex;

/**
 * Check if this vertex is highlighted (part of the selected batch and object)
 */ 
bool picking_isFragmentHighlighted() {
  return 
    bool(picking.isHighlightActive) &&
    picking.highlightedBatchIndex == picking.batchIndex &&
    picking.highlightedObjectIndex == picking_objectIndex
    ;
}

/**
 * Returns highlight color if this item is selected.
 */
vec4 picking_filterHighlightColor(vec4 color) {
  // If we are still picking, we don't highlight
  if (bool(picking.isActive)) {
    return color;
  }

  // If we are not highlighted, return color as is
  if (!picking_isFragmentHighlighted()) {
    return color;
  }
   
  // Blend in highlight color based on its alpha value
  float highLightAlpha = picking.highlightColor.a;
  float blendedAlpha = highLightAlpha + color.a * (1.0 - highLightAlpha);
  float highLightRatio = highLightAlpha / blendedAlpha;

  vec3 blendedRGB = mix(color.rgb, picking.highlightColor.rgb, highLightRatio);
  return vec4(blendedRGB, blendedAlpha);
}

/*
 * Returns picking color if picking enabled else unmodified argument.
 */
ivec4 picking_getPickingColor() {
  // Assumes that colorAttachment0 is rg32int
  // TODO? - we could render indices into a second color attachment and not mess with fragColor
  return ivec4(picking_objectIndex, picking.batchIndex, 0u, 0u);  
}

vec4 picking_filterPickingColor(vec4 color) {
  if (bool(picking.isActive)) {
    if (picking_objectIndex == INDEX_PICKING_INVALID_INDEX) {
      discard;
    }
  }
  return color;
}

/*
 * Returns picking color if picking is enabled if not
 * highlight color if this item is selected, otherwise unmodified argument.
 */
vec4 picking_filterColor(vec4 color) {
  vec4 outColor = color;
  outColor = picking_filterHighlightColor(outColor);
  outColor = picking_filterPickingColor(outColor);
  return outColor;
}
`
  );
  var picking = {
    ...pickingUniforms,
    name: "picking",
    source,
    vs,
    fs
  };

  // src/modules/picking/color-picking.ts
  var source2 = (
    /* wgsl */
    `${WGSL_UNIFORMS}
`
  );
  var vs2 = (
    /* glsl */
    `${GLSL_UNIFORMS}
out vec4 picking_vRGBcolor_Avalid;

// Normalize unsigned byte color to 0-1 range
vec3 picking_normalizeColor(vec3 color) {
  return picking.useFloatColors > 0.5 ? color : color / 255.0;
}

// Normalize unsigned byte color to 0-1 range
vec4 picking_normalizeColor(vec4 color) {
  return picking.useFloatColors > 0.5 ? color : color / 255.0;
}

bool picking_isColorZero(vec3 color) {
  return dot(color, vec3(1.0)) < 0.00001;
}

bool picking_isColorValid(vec3 color) {
  return dot(color, vec3(1.0)) > 0.00001;
}

// Check if this vertex is highlighted 
bool isVertexHighlighted(vec3 vertexColor) {
  vec3 highlightedObjectColor = picking_normalizeColor(picking.highlightedObjectColor);
  return
    bool(picking.isHighlightActive) && picking_isColorZero(abs(vertexColor - highlightedObjectColor));
}

// Set the current picking color
void picking_setPickingColor(vec3 pickingColor) {
  pickingColor = picking_normalizeColor(pickingColor);

  if (bool(picking.isActive)) {
    // Use alpha as the validity flag. If pickingColor is [0, 0, 0] fragment is non-pickable
    picking_vRGBcolor_Avalid.a = float(picking_isColorValid(pickingColor));

    if (!bool(picking.isAttribute)) {
      // Stores the picking color so that the fragment shader can render it during picking
      picking_vRGBcolor_Avalid.rgb = pickingColor;
    }
  } else {
    // Do the comparison with selected item color in vertex shader as it should mean fewer compares
    picking_vRGBcolor_Avalid.a = float(isVertexHighlighted(pickingColor));
  }
}

void picking_setObjectIndex(uint objectIndex) {
  if (bool(picking.isActive)) {
    uint index = objectIndex;
    if (picking.indexMode == PICKING_INDEX_MODE_INSTANCE) {
      index = uint(gl_InstanceID);
    }
    picking_vRGBcolor_Avalid.r = float(index % 255) / 255.0;
    picking_vRGBcolor_Avalid.g = float((index / 255) % 255) / 255.0;
    picking_vRGBcolor_Avalid.b = float((index / 255 / 255) %255) / 255.0;
  }
}

void picking_setPickingAttribute(float value) {
  if (bool(picking.isAttribute)) {
    picking_vRGBcolor_Avalid.r = value;
  }
}

void picking_setPickingAttribute(vec2 value) {
  if (bool(picking.isAttribute)) {
    picking_vRGBcolor_Avalid.rg = value;
  }
}

void picking_setPickingAttribute(vec3 value) {
  if (bool(picking.isAttribute)) {
    picking_vRGBcolor_Avalid.rgb = value;
  }
}
`
  );
  var fs2 = (
    /* glsl */
    `${GLSL_UNIFORMS}

in vec4 picking_vRGBcolor_Avalid;

/*
 * Returns highlight color if this item is selected.
 */
vec4 picking_filterHighlightColor(vec4 color) {
  // If we are still picking, we don't highlight
  if (picking.isActive > 0.5) {
    return color;
  }

  bool selected = bool(picking_vRGBcolor_Avalid.a);

  if (selected) {
    // Blend in highlight color based on its alpha value
    float highLightAlpha = picking.highlightColor.a;
    float blendedAlpha = highLightAlpha + color.a * (1.0 - highLightAlpha);
    float highLightRatio = highLightAlpha / blendedAlpha;

    vec3 blendedRGB = mix(color.rgb, picking.highlightColor.rgb, highLightRatio);
    return vec4(blendedRGB, blendedAlpha);
  } else {
    return color;
  }
}

/*
 * Returns picking color if picking enabled else unmodified argument.
 */
vec4 picking_filterPickingColor(vec4 color) {
  if (bool(picking.isActive)) {
    if (picking_vRGBcolor_Avalid.a == 0.0) {
      discard;
    }
    return picking_vRGBcolor_Avalid;
  }
  return color;
}

/*
 * Returns picking color if picking is enabled if not
 * highlight color if this item is selected, otherwise unmodified argument.
 */
vec4 picking_filterColor(vec4 color) {
  vec4 highlightColor = picking_filterHighlightColor(color);
  return picking_filterPickingColor(highlightColor);
}
`
  );
  var picking2 = {
    ...pickingUniforms,
    name: "picking",
    source: source2,
    vs: vs2,
    fs: fs2
  };

  // src/modules/picking/legacy-picking-manager.ts
  var LegacyPickingManager = class {
    device;
    framebuffer = null;
    shaderInputs;
    constructor(device, shaderInputs) {
      this.device = device;
      this.shaderInputs = shaderInputs;
    }
    destroy() {
      this.framebuffer?.destroy();
    }
    getFramebuffer() {
      if (!this.framebuffer) {
        this.framebuffer = this.device.createFramebuffer({
          colorAttachments: ["rgba8unorm"],
          depthStencilAttachment: "depth24plus"
        });
      }
      return this.framebuffer;
    }
    /** Clear highlighted / picked object */
    clearPickState() {
      this.shaderInputs.setProps({ picking: { highlightedObjectColor: null } });
    }
    /** Prepare for rendering picking colors */
    beginRenderPass() {
      const framebuffer = this.getFramebuffer();
      framebuffer.resize(this.device.getCanvasContext().getDevicePixelSize());
      this.shaderInputs.setProps({ picking: { isActive: true } });
      const pickingPass = this.device.beginRenderPass({
        framebuffer,
        clearColor: [0, 0, 0, 0],
        clearDepth: 1
      });
      return pickingPass;
    }
    updatePickState(mousePosition) {
      const framebuffer = this.getFramebuffer();
      const [pickX, pickY] = this.getPickPosition(mousePosition);
      const color255 = this.device.readPixelsToArrayWebGL(framebuffer, {
        sourceX: pickX,
        sourceY: pickY,
        sourceWidth: 1,
        sourceHeight: 1
      });
      let highlightedObjectColor = [...color255].map(
        (x) => x / 255
      );
      const isHighlightActive = highlightedObjectColor[0] + highlightedObjectColor[1] + highlightedObjectColor[2] > 0;
      if (!isHighlightActive) {
        highlightedObjectColor = null;
      }
      this.shaderInputs.setProps({
        picking: { isActive: false, highlightedObjectColor }
      });
    }
    /**
     * Get pick position in device pixel range
     * use the center pixel location in device pixel range
     */
    getPickPosition(mousePosition) {
      const devicePixels = this.device.getCanvasContext().cssToDevicePixels(mousePosition);
      const pickX = devicePixels.x + Math.floor(devicePixels.width / 2);
      const pickY = devicePixels.y + Math.floor(devicePixels.height / 2);
      return [pickX, pickY];
    }
  };
  return __toCommonJS(bundle_exports);
})();
      return __exports__;
      });
