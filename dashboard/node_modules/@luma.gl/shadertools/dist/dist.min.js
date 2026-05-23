(function webpackUniversalModuleDefinition(root, factory) {
  if (typeof exports === 'object' && typeof module === 'object')
    module.exports = factory();
  else if (typeof define === 'function' && define.amd) define([], factory);
        else if (typeof exports === 'object') exports['luma'] = factory();
  else root['luma'] = factory();})(globalThis, function () {
"use strict";var __exports__=(()=>{var yr=Object.create;var Ee=Object.defineProperty;var wr=Object.getOwnPropertyDescriptor;var kr=Object.getOwnPropertyNames;var Ir=Object.getPrototypeOf,Sr=Object.prototype.hasOwnProperty;var Ar=(a,e,t)=>e in a?Ee(a,e,{enumerable:!0,configurable:!0,writable:!0,value:t}):a[e]=t;var Tr=(a,e)=>()=>(e||a((e={exports:{}}).exports,e),e.exports),Lr=(a,e)=>{for(var t in e)Ee(a,t,{get:e[t],enumerable:!0})},Qe=(a,e,t,n)=>{if(e&&typeof e=="object"||typeof e=="function")for(let r of kr(e))!Sr.call(a,r)&&r!==t&&Ee(a,r,{get:()=>e[r],enumerable:!(n=wr(e,r))||n.enumerable});return a},Ye=(a,e,t)=>(Qe(a,e,"default"),t&&Qe(t,e,"default")),Rt=(a,e,t)=>(t=a!=null?yr(Ir(a)):{},Qe(e||!a||!a.__esModule?Ee(t,"default",{value:a,enumerable:!0}):t,a)),Er=a=>Qe(Ee({},"__esModule",{value:!0}),a);var vn=(a,e,t)=>(Ar(a,typeof e!="symbol"?e+"":e,t),t);var Je=Tr((Es,bn)=>{bn.exports=globalThis.luma});var Ze={};Lr(Ze,{ShaderAssembler:()=>De,_getDependencyGraph:()=>Fe,_resolveModules:()=>Cn,assembleGLSLShaderPair:()=>nt,capitalize:()=>pe,checkShaderModuleDeprecations:()=>Oe,combineInjects:()=>Ln,convertToVec4:()=>Gt,dirlight:()=>mn,fp32:()=>lr,fp64:()=>fr,fp64LowPart:()=>je,fp64arithmetic:()=>hn,fp64ify:()=>Te,fp64ifyMatrix4:()=>Xe,fromHalfFloat:()=>or,generateShaderForModule:()=>zn,getPassthroughFS:()=>Un,getQualifierDetails:()=>Vn,getShaderInfo:()=>tt,getShaderLayoutFromWGSL:()=>Jn,getShaderModuleDependencies:()=>Pe,getShaderModuleSource:()=>rt,getShaderModuleUniforms:()=>En,gouraudMaterial:()=>_n,initializeShaderModule:()=>et,initializeShaderModules:()=>ne,lighting:()=>te,pbrMaterial:()=>xr,phongMaterial:()=>gn,picking:()=>hr,preprocess:()=>st,random:()=>ar,toHalfFloat:()=>sr,typeToChannelCount:()=>Hn,typeToChannelSuffix:()=>$n});Ye(Ze,Rt(Je(),1));function le(a,e){if(!a)throw new Error(e||"shadertools: assertion failed.")}var Bt={number:{type:"number",validate(a,e){return Number.isFinite(a)&&typeof e=="object"&&(e.max===void 0||a<=e.max)&&(e.min===void 0||a>=e.min)}},array:{type:"array",validate(a,e){return Array.isArray(a)||ArrayBuffer.isView(a)}}};function yn(a){let e={};for(let[t,n]of Object.entries(a))e[t]=Cr(n);return e}function wn(a,e,t){let n={};for(let[r,s]of Object.entries(e))a&&r in a&&!s.private?(s.validate&&le(s.validate(a[r],s),`${t}: invalid ${r}`),n[r]=a[r]):n[r]=s.value;return n}function Cr(a){let e=xn(a);if(e!=="object")return{value:a,...Bt[e],type:e};if(typeof a=="object")return a?a.type!==void 0?{...a,...Bt[a.type],type:a.type}:a.value===void 0?{type:"object",value:a}:(e=xn(a.value),{...a,...Bt[e],type:e}):{type:"object",value:null};throw new Error("props")}function xn(a){return Array.isArray(a)||ArrayBuffer.isView(a)?"array":typeof a}var kn=`#ifdef MODULE_LOGDEPTH
  logdepth_adjustPosition(gl_Position);
#endif
`,In=`#ifdef MODULE_MATERIAL
  fragColor = material_filterColor(fragColor);
#endif

#ifdef MODULE_LIGHTING
  fragColor = lighting_filterColor(fragColor);
#endif

#ifdef MODULE_FOG
  fragColor = fog_filterColor(fragColor);
#endif

#ifdef MODULE_PICKING
  fragColor = picking_filterHighlightColor(fragColor);
  fragColor = picking_filterPickingColor(fragColor);
#endif

#ifdef MODULE_LOGDEPTH
  logdepth_setFragDepth();
#endif
`;var Nr={vertex:kn,fragment:In},Sn=/void\s+main\s*\([^)]*\)\s*\{\n?/,An=/}\n?[^{}]*$/,Vt=[],Ce="__LUMA_INJECT_DECLARATIONS__";function Tn(a){let e={vertex:{},fragment:{}};for(let t in a){let n=a[t],r=Or(t);typeof n=="string"&&(n={order:0,injection:n}),e[r][t]=n}return e}function Or(a){let e=a.slice(0,2);switch(e){case"vs":return"vertex";case"fs":return"fragment";default:throw new Error(e)}}function Ne(a,e,t,n=!1){let r=e==="vertex";for(let s in t){let i=t[s];i.sort((l,c)=>l.order-c.order),Vt.length=i.length;for(let l=0,c=i.length;l<c;++l)Vt[l]=i[l].injection;let o=`${Vt.join(`
`)}
`;switch(s){case"vs:#decl":r&&(a=a.replace(Ce,o));break;case"vs:#main-start":r&&(a=a.replace(Sn,l=>l+o));break;case"vs:#main-end":r&&(a=a.replace(An,l=>o+l));break;case"fs:#decl":r||(a=a.replace(Ce,o));break;case"fs:#main-start":r||(a=a.replace(Sn,l=>l+o));break;case"fs:#main-end":r||(a=a.replace(An,l=>o+l));break;default:a=a.replace(s,l=>l+o)}}return a=a.replace(Ce,""),n&&(a=a.replace(/\}\s*$/,s=>s+Nr[e])),a}function Ln(a){let e={};return le(Array.isArray(a)&&a.length>1),a.forEach(t=>{for(let n in t)e[n]=e[n]?`${e[n]}
${t[n]}`:t[n]}),e}function ne(a){a.map(e=>et(e))}function et(a){if(a.instance)return;ne(a.dependencies||[]);let{propTypes:e={},deprecations:t=[],inject:n={}}=a,r={normalizedInjections:Tn(n),parsedDeprecations:Pr(t)};e&&(r.propValidators=yn(e)),a.instance=r;let s={};e&&(s=Object.entries(e).reduce((i,[o,l])=>{let c=l?.value;return c&&(i[o]=c),i},{})),a.defaultUniforms={...a.defaultUniforms,...s}}function En(a,e,t){et(a);let n=t||{...a.defaultUniforms};return e&&a.getUniforms?a.getUniforms(e,n):wn(e,a.instance?.propValidators,a.name)}function Oe(a,e,t){a.deprecations?.forEach(n=>{n.regex?.test(e)&&(n.deprecated?t.deprecated(n.old,n.new)():t.removed(n.old,n.new)())})}function Pr(a){return a.forEach(e=>{switch(e.type){case"function":e.regex=new RegExp(`\\b${e.old}\\(`);break;default:e.regex=new RegExp(`${e.type} ${e.old};`)}}),a}function Pe(a){ne(a);let e={},t={};Fe({modules:a,level:0,moduleMap:e,moduleDepth:t});let n=Object.keys(t).sort((r,s)=>t[s]-t[r]).map(r=>e[r]);return ne(n),n}function Fe(a){let{modules:e,level:t,moduleMap:n,moduleDepth:r}=a;if(t>=5)throw new Error("Possible loop in shader dependency graph");for(let s of e)n[s.name]=s,(r[s.name]===void 0||r[s.name]<t)&&(r[s.name]=t);for(let s of e)s.dependencies&&Fe({modules:s.dependencies,level:t+1,moduleMap:n,moduleDepth:r})}function Fr(a){ne(a);let e={},t={};return Fe({modules:a,level:0,moduleMap:e,moduleDepth:t}),a=Object.keys(t).sort((n,r)=>t[r]-t[n]).map(n=>e[n]),ne(a),a}function Cn(a){return Fr(a)}function Nn(a){switch(a?.gpu.toLowerCase()){case"apple":return`#define APPLE_GPU
// Apple optimizes away the calculation necessary for emulated fp64
#define LUMA_FP64_CODE_ELIMINATION_WORKAROUND 1
#define LUMA_FP32_TAN_PRECISION_WORKAROUND 1
// Intel GPU doesn't have full 32 bits precision in same cases, causes overflow
#define LUMA_FP64_HIGH_BITS_OVERFLOW_WORKAROUND 1
`;case"nvidia":return`#define NVIDIA_GPU
// Nvidia optimizes away the calculation necessary for emulated fp64
#define LUMA_FP64_CODE_ELIMINATION_WORKAROUND 1
`;case"intel":return`#define INTEL_GPU
// Intel optimizes away the calculation necessary for emulated fp64
#define LUMA_FP64_CODE_ELIMINATION_WORKAROUND 1
// Intel's built-in 'tan' function doesn't have acceptable precision
#define LUMA_FP32_TAN_PRECISION_WORKAROUND 1
// Intel GPU doesn't have full 32 bits precision in same cases, causes overflow
#define LUMA_FP64_HIGH_BITS_OVERFLOW_WORKAROUND 1
`;case"amd":return`#define AMD_GPU
`;default:return`#define DEFAULT_GPU
// Prevent driver from optimizing away the calculation necessary for emulated fp64
#define LUMA_FP64_CODE_ELIMINATION_WORKAROUND 1
// Headless Chrome's software shader 'tan' function doesn't have acceptable precision
#define LUMA_FP32_TAN_PRECISION_WORKAROUND 1
// If the GPU doesn't have full 32 bits precision, will causes overflow
#define LUMA_FP64_HIGH_BITS_OVERFLOW_WORKAROUND 1
`}}function Pn(a,e){if(Number(a.match(/^#version[ \t]+(\d+)/m)?.[1]||100)!==300)throw new Error("luma.gl v9 only supports GLSL 3.00 shader sources");switch(e){case"vertex":return a=On(a,Dr),a;case"fragment":return a=On(a,Mr),a;default:throw new Error(e)}}var Fn=[[/^(#version[ \t]+(100|300[ \t]+es))?[ \t]*\n/,`#version 300 es
`],[/\btexture(2D|2DProj|Cube)Lod(EXT)?\(/g,"textureLod("],[/\btexture(2D|2DProj|Cube)(EXT)?\(/g,"texture("]],Dr=[...Fn,[Ut("attribute"),"in $1"],[Ut("varying"),"out $1"]],Mr=[...Fn,[Ut("varying"),"in $1"]];function On(a,e){for(let[t,n]of e)a=a.replace(t,n);return a}function Ut(a){return new RegExp(`\\b${a}[ \\t]+(\\w+[ \\t]+\\w+(\\[\\w+\\])?;)`,"g")}function $t(a,e){let t="";for(let n in a){let r=a[n];if(t+=`void ${r.signature} {
`,r.header&&(t+=`  ${r.header}`),e[n]){let s=e[n];s.sort((i,o)=>i.order-o.order);for(let i of s)t+=`  ${i.injection}
`}r.footer&&(t+=`  ${r.footer}`),t+=`}
`}return t}function Ht(a){let e={vertex:{},fragment:{}};for(let t of a){let n,r;typeof t!="string"?(n=t,r=n.hook):(n={},r=t),r=r.trim();let[s,i]=r.split(":"),o=r.replace(/\(.+/,""),l=Object.assign(n,{signature:i});switch(s){case"vs":e.vertex[o]=l;break;case"fs":e.fragment[o]=l;break;default:throw new Error(s)}}return e}function tt(a,e){return{name:Rr(a,e),language:"glsl",version:Br(a)}}function Rr(a,e="unnamed"){let n=/#define[^\S\r\n]*SHADER_NAME[^\S\r\n]*([A-Za-z0-9_-]+)\s*/.exec(a);return n?n[1]:e}function Br(a){let e=100,t=a.match(/[^\s]+/g);if(t&&t.length>=2&&t[0]==="#version"){let n=parseInt(t[1],10);Number.isFinite(n)&&(e=n)}if(e!==100&&e!==300)throw new Error(`Invalid GLSL version ${e}`);return e}var Mn=`

${Ce}
`,Vr=`precision highp float;
`;function Rn(a){let e=Pe(a.modules||[]);return{source:Ur(a.platformInfo,{...a,source:a.source,stage:"vertex",modules:e}),getUniforms:Bn(e)}}function nt(a){let{vs:e,fs:t}=a,n=Pe(a.modules||[]);return{vs:Dn(a.platformInfo,{...a,source:e,stage:"vertex",modules:n}),fs:Dn(a.platformInfo,{...a,source:t,stage:"fragment",modules:n}),getUniforms:Bn(n)}}function Ur(a,e){let{source:t,stage:n,modules:r,hookFunctions:s=[],inject:i={},log:o}=e;le(typeof t=="string","shader source must be a string");let l=t,c="",u=Ht(s),h={},b={},y={};for(let E in i){let T=typeof i[E]=="string"?{injection:i[E],order:0}:i[E],L=/^(v|f)s:(#)?([\w-]+)$/.exec(E);if(L){let P=L[2],k=L[3];P?k==="decl"?b[E]=[T]:y[E]=[T]:h[E]=[T]}else y[E]=[T]}let S=r;for(let E of S){o&&Oe(E,l,o);let T=rt(E,"wgsl");c+=T;let L=E.injections?.[n]||{};for(let P in L){let k=/^(v|f)s:#([\w-]+)$/.exec(P);if(k){let B=k[2]==="decl"?b:y;B[P]=B[P]||[],B[P].push(L[P])}else h[P]=h[P]||[],h[P].push(L[P])}}return c+=Mn,c=Ne(c,n,b),c+=$t(u[n],h),c+=l,c=Ne(c,n,y),c}function Dn(a,e){let{source:t,stage:n,language:r="glsl",modules:s,defines:i={},hookFunctions:o=[],inject:l={},prologue:c=!0,log:u}=e;le(typeof t=="string","shader source must be a string");let h=r==="glsl"?tt(t).version:-1,b=a.shaderLanguageVersion,y=h===100?"#version 100":"#version 300 es",E=t.split(`
`).slice(1).join(`
`),T={};s.forEach(O=>{Object.assign(T,O.defines)}),Object.assign(T,i);let L="";switch(r){case"wgsl":break;case"glsl":L=c?`${y}

// ----- PROLOGUE -------------------------
${`#define SHADER_TYPE_${n.toUpperCase()}`}

${Nn(a)}
${n==="fragment"?Vr:""}

// ----- APPLICATION DEFINES -------------------------

${$r(T)}

`:`${y}
`;break}let P=Ht(o),k={},A={},B={};for(let O in l){let oe=typeof l[O]=="string"?{injection:l[O],order:0}:l[O],X=/^(v|f)s:(#)?([\w-]+)$/.exec(O);if(X){let U=X[2],he=X[3];U?he==="decl"?A[O]=[oe]:B[O]=[oe]:k[O]=[oe]}else B[O]=[oe]}for(let O of s){u&&Oe(O,E,u);let oe=rt(O,n);L+=oe;let X=O.instance?.normalizedInjections[n]||{};for(let U in X){let he=/^(v|f)s:#([\w-]+)$/.exec(U);if(he){let Mt=he[2]==="decl"?A:B;Mt[U]=Mt[U]||[],Mt[U].push(X[U])}else k[U]=k[U]||[],k[U].push(X[U])}}return L+="// ----- MAIN SHADER SOURCE -------------------------",L+=Mn,L=Ne(L,n,A),L+=$t(P[n],k),L+=E,L=Ne(L,n,B),r==="glsl"&&h!==b&&(L=Pn(L,n)),L.trim()}function Bn(a){return function(t){let n={};for(let r of a){let s=r.getUniforms?.(t,n);Object.assign(n,s)}return n}}function $r(a={}){let e="";for(let t in a){let n=a[t];(n||Number.isFinite(n))&&(e+=`#define ${t.toUpperCase()} ${a[t]}
`)}return e}function rt(a,e){let t;switch(e){case"vertex":t=a.vs||"";break;case"fragment":t=a.fs||"";break;case"wgsl":t=a.source||"";break;default:le(!1)}if(!a.name)throw new Error("Shader module must have a name");let n=a.name.toUpperCase().replace(/[^0-9a-z]/gi,"_"),r=`// ----- MODULE ${a.name} ---------------

`;return e!=="wgsl"&&(r+=`#define MODULE_${n}
`),r+=`${t}
`,r}var Hr=/^\s*\#\s*ifdef\s*([a-zA-Z_]+)\s*$/,Gr=/^\s*\#\s*endif\s*$/;function st(a,e){let t=a.split(`
`),n=[],r=!0,s=null;for(let i of t){let o=i.match(Hr),l=i.match(Gr);o?(s=o[1],r=Boolean(e?.defines?.[s])):l?r=!0:r&&n.push(i)}return n.join(`
`)}var be=class{_hookFunctions=[];_defaultModules=[];static getDefaultShaderAssembler(){return be.defaultShaderAssembler=be.defaultShaderAssembler||new be,be.defaultShaderAssembler}addDefaultModule(e){this._defaultModules.find(t=>t.name===(typeof e=="string"?e:e.name))||this._defaultModules.push(e)}removeDefaultModule(e){let t=typeof e=="string"?e:e.name;this._defaultModules=this._defaultModules.filter(n=>n.name!==t)}addShaderHook(e,t){t&&(e=Object.assign(t,{hook:e})),this._hookFunctions.push(e)}assembleWGSLShader(e){let t=this._getModuleList(e.modules),n=this._hookFunctions,{source:r,getUniforms:s}=Rn({...e,source:e.source,modules:t,hookFunctions:n});return{source:e.platformInfo.shaderLanguage==="wgsl"?st(r):r,getUniforms:s,modules:t}}assembleGLSLShaderPair(e){let t=this._getModuleList(e.modules),n=this._hookFunctions;return{...nt({...e,vs:e.vs,fs:e.fs,modules:t,hookFunctions:n}),modules:t}}_getModuleList(e=[]){let t=new Array(this._defaultModules.length+e.length),n={},r=0;for(let s=0,i=this._defaultModules.length;s<i;++s){let o=this._defaultModules[s],l=o.name;t[r++]=o,n[l]=!0}for(let s=0,i=e.length;s<i;++s){let o=e[s],l=o.name;n[l]||(t[r++]=o,n[l]=!0)}return t.length=r,ne(t),t}},De=be;vn(De,"defaultShaderAssembler");var Wr=`out vec4 transform_output;
void main() {
  transform_output = vec4(0);
}`,zr=`#version 300 es
${Wr}`;function Vn(a,e){e=Array.isArray(e)?e:[e];let t=a.replace(/^\s+/,"").split(/\s+/),[n,r,s]=t;if(!e.includes(n)||!r||!s)return null;let i=s.split(";")[0];return{qualifier:n,type:r,name:i}}function Un(a){let{input:e,inputChannels:t,output:n}=a||{};if(!e)return zr;if(!t)throw new Error("inputChannels");let r=qr(t),s=Gt(e,t);return`#version 300 es
in ${r} ${e};
out vec4 ${n};
void main() {
  ${n} = ${s};
}`}function $n(a){switch(a){case"float":return"x";case"vec2":return"xy";case"vec3":return"xyz";case"vec4":return"xyzw";default:throw new Error(a)}}function Hn(a){switch(a){case"float":return 1;case"vec2":return 2;case"vec3":return 3;case"vec4":return 4;default:throw new Error(a)}}function qr(a){switch(a){case 1:return"float";case 2:return"vec2";case 3:return"vec3";case 4:return"vec4";default:throw new Error(`invalid channels: ${a}`)}}function Gt(a,e){switch(e){case 1:return`vec4(${a}, 0.0, 0.0, 1.0)`;case 2:return`vec4(${a}, 0.0, 1.0)`;case 3:return`vec4(${a}, 1.0)`;case 4:return a;default:throw new Error(`invalid channels: ${e}`)}}function pe(a){return typeof a=="string"?a.charAt(0).toUpperCase()+a.slice(1):a}function Gn(a,e){return jr(a,e)}function jr(a,e){let t=[];switch(e.uniforms){case"scoped-interface-blocks":case"unscoped-interface-blocks":t.push(`uniform ${pe(a.name)} {`);break;case"uniforms":}for(let[n,r]of Object.entries(a.uniformTypes||{})){let s=Xr(r);switch(e.uniforms){case"scoped-interface-blocks":t.push(`  ${s} ${n};`);break;case"unscoped-interface-blocks":t.push(`  ${s} ${a.name}_${n};`);break;case"uniforms":t.push(`uniform ${s} ${a.name}_${n};`)}}switch(e.uniforms){case"scoped-interface-blocks":t.push(`} ${a.name};`);break;case"unscoped-interface-blocks":t.push("};");break;case"uniforms":}return t.push(""),t.join(`
`)}function Xr(a){return{f32:"float",i32:"int",u32:"uint","vec2<f32>":"vec2","vec3<f32>":"vec3","vec4<f32>":"vec4","vec2<i32>":"ivec2","vec3<i32>":"ivec3","vec4<i32>":"ivec4","vec2<u32>":"uvec2","vec3<u32>":"uvec3","vec4<u32>":"uvec4","mat2x2<f32>":"mat2","mat2x3<f32>":"mat2x3","mat2x4<f32>":"mat2x4","mat3x2<f32>":"mat3x2","mat3x3<f32>":"mat3","mat3x4<f32>":"mat3x4","mat4x2<f32>":"mat4x2","mat4x3<f32>":"mat4x3","mat4x4<f32>":"mat4"}[a]}function Wn(a,e){return Kr(a,e)}function Kr(a,e){let t=[];t.push(`struct ${pe(a.name)} {`);for(let[n,r]of Object.entries(a?.uniformTypes||{})){let s=r;t.push(`  ${n} : ${s};`)}return t.push("};"),t.push(`var<uniform> ${a.name} : ${pe(a.name)};`),t.join(`
`)}function zn(a,e){switch(e.shaderLanguage){case"glsl":return Gn(a,e);case"wgsl":return Wn(a,e)}}var Yn=Rt(Je(),1);var W=class{constructor(e,t){this.name=e,this.attributes=t,this.size=0}get isArray(){return!1}get isStruct(){return!1}get isTemplate(){return!1}getTypeName(){return this.name}},ot=class{constructor(e,t,n){this.name=e,this.type=t,this.attributes=n,this.offset=0,this.size=0}get isArray(){return this.type.isArray}get isStruct(){return this.type.isStruct}get isTemplate(){return this.type.isTemplate}get align(){return this.type.isStruct?this.type.align:0}get members(){return this.type.isStruct?this.type.members:null}get format(){return this.type.isArray||this.type.isTemplate?this.type.format:null}get count(){return this.type.isArray?this.type.count:0}get stride(){return this.type.isArray?this.type.stride:this.size}},re=class extends W{constructor(e,t){super(e,t),this.members=[],this.align=0,this.startLine=-1,this.endLine=-1,this.inUse=!1}get isStruct(){return!0}},se=class extends W{constructor(e,t){super(e,t),this.count=0,this.stride=0}get isArray(){return!0}},ie=class extends W{constructor(e,t,n,r){super(e,n),this.format=t,this.access=r}get isTemplate(){return!0}getTypeName(){let e=this.name;if(this.format!==null){if(e==="vec2"||e==="vec3"||e==="vec4"||e==="mat2x2"||e==="mat2x3"||e==="mat2x4"||e==="mat3x2"||e==="mat3x3"||e==="mat3x4"||e==="mat4x2"||e==="mat4x3"||e==="mat4x4"){if(this.format.name==="f32")return e+="f",e;if(this.format.name==="i32")return e+="i",e;if(this.format.name==="u32")return e+="u",e;if(this.format.name==="bool")return e+="b",e;if(this.format.name==="f16")return e+="h",e}e+=`<${this.format.name}>`}else if(e==="vec2"||e==="vec3"||e==="vec4")return e;return e}},ce;(a=>{a[a.Uniform=0]="Uniform",a[a.Storage=1]="Storage",a[a.Texture=2]="Texture",a[a.Sampler=3]="Sampler",a[a.StorageTexture=4]="StorageTexture"})(ce||(ce={}));var xe=class{constructor(e,t,n,r,s,i,o){this.name=e,this.type=t,this.group=n,this.binding=r,this.attributes=s,this.resourceType=i,this.access=o}get isArray(){return this.type.isArray}get isStruct(){return this.type.isStruct}get isTemplate(){return this.type.isTemplate}get size(){return this.type.size}get align(){return this.type.isStruct?this.type.align:0}get members(){return this.type.isStruct?this.type.members:null}get format(){return this.type.isArray||this.type.isTemplate?this.type.format:null}get count(){return this.type.isArray?this.type.count:0}get stride(){return this.type.isArray?this.type.stride:this.size}},qt=class{constructor(e,t){this.name=e,this.type=t}},jt=class{constructor(e,t,n,r){this.name=e,this.type=t,this.locationType=n,this.location=r,this.interpolation=null}},it=class{constructor(e,t,n,r){this.name=e,this.type=t,this.locationType=n,this.location=r}},Xt=class{constructor(e,t,n,r){this.name=e,this.type=t,this.attributes=n,this.id=r}},Kt=class{constructor(e,t,n){this.name=e,this.type=t,this.attributes=n}},Zt=class{constructor(e,t=null,n){this.stage=null,this.inputs=[],this.outputs=[],this.arguments=[],this.returnType=null,this.resources=[],this.overrides=[],this.startLine=-1,this.endLine=-1,this.inUse=!1,this.calls=new Set,this.name=e,this.stage=t,this.attributes=n}},Qt=class{constructor(){this.vertex=[],this.fragment=[],this.compute=[]}},Xn=new Float32Array(1),Zr=new Int32Array(Xn.buffer),$=new Uint16Array(1);function Qr(a){Xn[0]=a;let e=Zr[0],t=e>>31&1,n=e>>23&255,r=8388607&e;if(n===255)return $[0]=t<<15|31744|(r!==0?512:0),$[0];if(n===0){if(r===0)return $[0]=t<<15,$[0];r|=8388608;let s=113;for(;!(8388608&r);)r<<=1,s--;return n=127-s,r&=8388607,n>0?(r=(r>>126-n)+(r>>127-n&1),$[0]=t<<15|n<<10|r>>13,$[0]):($[0]=t<<15,$[0])}return n=n-127+15,n>=31?($[0]=t<<15|31744,$[0]):n<=0?n<-10?($[0]=t<<15,$[0]):(r=(8388608|r)>>1-n,$[0]=t<<15|r>>13,$[0]):(r>>=13,$[0]=t<<15|n<<10|r,$[0])}var un=new Uint32Array(1),Kn=new Float32Array(un.buffer,0,1);function qn(a){let e=112+(a>>6&31)<<23|(63&a)<<17;return un[0]=e,Kn[0]}function Yr(a,e,t,n,r,s,i,o,l){let c=n*(i>>=r)*(s>>=r)+t*i+e*o;switch(l){case"r8unorm":return[C(a,c,"8unorm",1)[0]];case"r8snorm":return[C(a,c,"8snorm",1)[0]];case"r8uint":return[C(a,c,"8uint",1)[0]];case"r8sint":return[C(a,c,"8sint",1)[0]];case"rg8unorm":{let u=C(a,c,"8unorm",2);return[u[0],u[1]]}case"rg8snorm":{let u=C(a,c,"8snorm",2);return[u[0],u[1]]}case"rg8uint":{let u=C(a,c,"8uint",2);return[u[0],u[1]]}case"rg8sint":{let u=C(a,c,"8sint",2);return[u[0],u[1]]}case"rgba8unorm-srgb":case"rgba8unorm":{let u=C(a,c,"8unorm",4);return[u[0],u[1],u[2],u[3]]}case"rgba8snorm":{let u=C(a,c,"8snorm",4);return[u[0],u[1],u[2],u[3]]}case"rgba8uint":{let u=C(a,c,"8uint",4);return[u[0],u[1],u[2],u[3]]}case"rgba8sint":{let u=C(a,c,"8sint",4);return[u[0],u[1],u[2],u[3]]}case"bgra8unorm-srgb":case"bgra8unorm":{let u=C(a,c,"8unorm",4);return[u[2],u[1],u[0],u[3]]}case"r16uint":return[C(a,c,"16uint",1)[0]];case"r16sint":return[C(a,c,"16sint",1)[0]];case"r16float":return[C(a,c,"16float",1)[0]];case"rg16uint":{let u=C(a,c,"16uint",2);return[u[0],u[1]]}case"rg16sint":{let u=C(a,c,"16sint",2);return[u[0],u[1]]}case"rg16float":{let u=C(a,c,"16float",2);return[u[0],u[1]]}case"rgba16uint":{let u=C(a,c,"16uint",4);return[u[0],u[1],u[2],u[3]]}case"rgba16sint":{let u=C(a,c,"16sint",4);return[u[0],u[1],u[2],u[3]]}case"rgba16float":{let u=C(a,c,"16float",4);return[u[0],u[1],u[2],u[3]]}case"r32uint":return[C(a,c,"32uint",1)[0]];case"r32sint":return[C(a,c,"32sint",1)[0]];case"depth16unorm":case"depth24plus":case"depth24plus-stencil8":case"depth32float":case"depth32float-stencil8":case"r32float":return[C(a,c,"32float",1)[0]];case"rg32uint":{let u=C(a,c,"32uint",2);return[u[0],u[1]]}case"rg32sint":{let u=C(a,c,"32sint",2);return[u[0],u[1]]}case"rg32float":{let u=C(a,c,"32float",2);return[u[0],u[1]]}case"rgba32uint":{let u=C(a,c,"32uint",4);return[u[0],u[1],u[2],u[3]]}case"rgba32sint":{let u=C(a,c,"32sint",4);return[u[0],u[1],u[2],u[3]]}case"rgba32float":{let u=C(a,c,"32float",4);return[u[0],u[1],u[2],u[3]]}case"rg11b10ufloat":{let u=new Uint32Array(a.buffer,c,1)[0],h=(4192256&u)>>11,b=(4290772992&u)>>22;return[qn(2047&u),qn(h),function(y){let S=112+(y>>5&31)<<23|(31&y)<<18;return un[0]=S,Kn[0]}(b),1]}}return null}function C(a,e,t,n){let r=[0,0,0,0];for(let c=0;c<n;++c)switch(t){case"8unorm":r[c]=a[e]/255,e++;break;case"8snorm":r[c]=a[e]/255*2-1,e++;break;case"8uint":r[c]=a[e],e++;break;case"8sint":r[c]=a[e]-127,e++;break;case"16uint":r[c]=a[e]|a[e+1]<<8,e+=2;break;case"16sint":r[c]=(a[e]|a[e+1]<<8)-32768,e+=2;break;case"16float":r[c]=(s=a[e]|a[e+1]<<8,i=void 0,o=void 0,l=void 0,i=(32768&s)>>15,l=1023&s,(o=(31744&s)>>10)==0?(i?-1:1)*Math.pow(2,-14)*(l/Math.pow(2,10)):o==31?l?NaN:1/0*(i?-1:1):(i?-1:1)*Math.pow(2,o-15)*(1+l/Math.pow(2,10))),e+=2;break;case"32uint":case"32sint":r[c]=a[e]|a[e+1]<<8|a[e+2]<<16|a[e+3]<<24,e+=4;break;case"32float":r[c]=new Float32Array(a.buffer,e,1)[0],e+=4}var s,i,o,l;return r}function N(a,e,t,n,r){for(let s=0;s<n;++s)switch(t){case"8unorm":a[e]=255*r[s],e++;break;case"8snorm":a[e]=.5*(r[s]+1)*255,e++;break;case"8uint":a[e]=r[s],e++;break;case"8sint":a[e]=r[s]+127,e++;break;case"16uint":new Uint16Array(a.buffer,e,1)[0]=r[s],e+=2;break;case"16sint":new Int16Array(a.buffer,e,1)[0]=r[s],e+=2;break;case"16float":{let i=Qr(r[s]);new Uint16Array(a.buffer,e,1)[0]=i,e+=2;break}case"32uint":new Uint32Array(a.buffer,e,1)[0]=r[s],e+=4;break;case"32sint":new Int32Array(a.buffer,e,1)[0]=r[s],e+=4;break;case"32float":new Float32Array(a.buffer,e,1)[0]=r[s],e+=4}return r}var Wt={r8unorm:{bytesPerBlock:1,blockWidth:1,blockHeight:1,isCompressed:!1,channels:1},r8snorm:{bytesPerBlock:1,blockWidth:1,blockHeight:1,isCompressed:!1,channels:1},r8uint:{bytesPerBlock:1,blockWidth:1,blockHeight:1,isCompressed:!1,channels:1},r8sint:{bytesPerBlock:1,blockWidth:1,blockHeight:1,isCompressed:!1,channels:1},rg8unorm:{bytesPerBlock:2,blockWidth:1,blockHeight:1,isCompressed:!1,channels:2},rg8snorm:{bytesPerBlock:2,blockWidth:1,blockHeight:1,isCompressed:!1,channels:2},rg8uint:{bytesPerBlock:2,blockWidth:1,blockHeight:1,isCompressed:!1,channels:2},rg8sint:{bytesPerBlock:2,blockWidth:1,blockHeight:1,isCompressed:!1,channels:2},rgba8unorm:{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,channels:4},"rgba8unorm-srgb":{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,channels:4},rgba8snorm:{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,channels:4},rgba8uint:{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,channels:4},rgba8sint:{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,channels:4},bgra8unorm:{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,channels:4},"bgra8unorm-srgb":{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,channels:4},r16uint:{bytesPerBlock:2,blockWidth:1,blockHeight:1,isCompressed:!1,channels:1},r16sint:{bytesPerBlock:2,blockWidth:1,blockHeight:1,isCompressed:!1,channels:1},r16float:{bytesPerBlock:2,blockWidth:1,blockHeight:1,isCompressed:!1,channels:1},rg16uint:{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,channels:2},rg16sint:{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,channels:2},rg16float:{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,channels:2},rgba16uint:{bytesPerBlock:8,blockWidth:1,blockHeight:1,isCompressed:!1,channels:4},rgba16sint:{bytesPerBlock:8,blockWidth:1,blockHeight:1,isCompressed:!1,channels:4},rgba16float:{bytesPerBlock:8,blockWidth:1,blockHeight:1,isCompressed:!1,channels:4},r32uint:{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,channels:1},r32sint:{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,channels:1},r32float:{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,channels:1},rg32uint:{bytesPerBlock:8,blockWidth:1,blockHeight:1,isCompressed:!1,channels:2},rg32sint:{bytesPerBlock:8,blockWidth:1,blockHeight:1,isCompressed:!1,channels:2},rg32float:{bytesPerBlock:8,blockWidth:1,blockHeight:1,isCompressed:!1,channels:2},rgba32uint:{bytesPerBlock:16,blockWidth:1,blockHeight:1,isCompressed:!1,channels:4},rgba32sint:{bytesPerBlock:16,blockWidth:1,blockHeight:1,isCompressed:!1,channels:4},rgba32float:{bytesPerBlock:16,blockWidth:1,blockHeight:1,isCompressed:!1,channels:4},rgb10a2uint:{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,channels:4},rgb10a2unorm:{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,channels:4},rg11b10ufloat:{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,channels:4},stencil8:{bytesPerBlock:1,blockWidth:1,blockHeight:1,isCompressed:!1,isDepthStencil:!0,hasDepth:!1,hasStencil:!0,channels:1},depth16unorm:{bytesPerBlock:2,blockWidth:1,blockHeight:1,isCompressed:!1,isDepthStencil:!0,hasDepth:!0,hasStencil:!1,channels:1},depth24plus:{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,isDepthStencil:!0,hasDepth:!0,hasStencil:!1,depthOnlyFormat:"depth32float",channels:1},"depth24plus-stencil8":{bytesPerBlock:8,blockWidth:1,blockHeight:1,isCompressed:!1,isDepthStencil:!0,hasDepth:!0,hasStencil:!0,depthOnlyFormat:"depth32float",channels:1},depth32float:{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,isDepthStencil:!0,hasDepth:!0,hasStencil:!1,channels:1},"depth32float-stencil8":{bytesPerBlock:8,blockWidth:1,blockHeight:1,isCompressed:!1,isDepthStencil:!0,hasDepth:!0,hasStencil:!0,stencilOnlyFormat:"depth32float",channels:1},rgb9e5ufloat:{bytesPerBlock:4,blockWidth:1,blockHeight:1,isCompressed:!1,channels:4},"bc1-rgba-unorm":{bytesPerBlock:8,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"bc1-rgba-unorm-srgb":{bytesPerBlock:8,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"bc2-rgba-unorm":{bytesPerBlock:16,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"bc2-rgba-unorm-srgb":{bytesPerBlock:16,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"bc3-rgba-unorm":{bytesPerBlock:16,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"bc3-rgba-unorm-srgb":{bytesPerBlock:16,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"bc4-r-unorm":{bytesPerBlock:8,blockWidth:4,blockHeight:4,isCompressed:!0,channels:1},"bc4-r-snorm":{bytesPerBlock:8,blockWidth:4,blockHeight:4,isCompressed:!0,channels:1},"bc5-rg-unorm":{bytesPerBlock:16,blockWidth:4,blockHeight:4,isCompressed:!0,channels:2},"bc5-rg-snorm":{bytesPerBlock:16,blockWidth:4,blockHeight:4,isCompressed:!0,channels:2},"bc6h-rgb-ufloat":{bytesPerBlock:16,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"bc6h-rgb-float":{bytesPerBlock:16,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"bc7-rgba-unorm":{bytesPerBlock:16,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"bc7-rgba-unorm-srgb":{bytesPerBlock:16,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"etc2-rgb8unorm":{bytesPerBlock:8,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"etc2-rgb8unorm-srgb":{bytesPerBlock:8,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"etc2-rgb8a1unorm":{bytesPerBlock:8,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"etc2-rgb8a1unorm-srgb":{bytesPerBlock:8,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"etc2-rgba8unorm":{bytesPerBlock:16,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"etc2-rgba8unorm-srgb":{bytesPerBlock:16,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"eac-r11unorm":{bytesPerBlock:8,blockWidth:1,blockHeight:1,isCompressed:!0,channels:1},"eac-r11snorm":{bytesPerBlock:8,blockWidth:1,blockHeight:1,isCompressed:!0,channels:1},"eac-rg11unorm":{bytesPerBlock:16,blockWidth:1,blockHeight:1,isCompressed:!0,channels:2},"eac-rg11snorm":{bytesPerBlock:16,blockWidth:1,blockHeight:1,isCompressed:!0,channels:2},"astc-4x4-unorm":{bytesPerBlock:16,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"astc-4x4-unorm-srgb":{bytesPerBlock:16,blockWidth:4,blockHeight:4,isCompressed:!0,channels:4},"astc-5x4-unorm":{bytesPerBlock:16,blockWidth:5,blockHeight:4,isCompressed:!0,channels:4},"astc-5x4-unorm-srgb":{bytesPerBlock:16,blockWidth:5,blockHeight:4,isCompressed:!0,channels:4},"astc-5x5-unorm":{bytesPerBlock:16,blockWidth:5,blockHeight:5,isCompressed:!0,channels:4},"astc-5x5-unorm-srgb":{bytesPerBlock:16,blockWidth:5,blockHeight:5,isCompressed:!0,channels:4},"astc-6x5-unorm":{bytesPerBlock:16,blockWidth:6,blockHeight:5,isCompressed:!0,channels:4},"astc-6x5-unorm-srgb":{bytesPerBlock:16,blockWidth:6,blockHeight:5,isCompressed:!0,channels:4},"astc-6x6-unorm":{bytesPerBlock:16,blockWidth:6,blockHeight:6,isCompressed:!0,channels:4},"astc-6x6-unorm-srgb":{bytesPerBlock:16,blockWidth:6,blockHeight:6,isCompressed:!0,channels:4},"astc-8x5-unorm":{bytesPerBlock:16,blockWidth:8,blockHeight:5,isCompressed:!0,channels:4},"astc-8x5-unorm-srgb":{bytesPerBlock:16,blockWidth:8,blockHeight:5,isCompressed:!0,channels:4},"astc-8x6-unorm":{bytesPerBlock:16,blockWidth:8,blockHeight:6,isCompressed:!0,channels:4},"astc-8x6-unorm-srgb":{bytesPerBlock:16,blockWidth:8,blockHeight:6,isCompressed:!0,channels:4},"astc-8x8-unorm":{bytesPerBlock:16,blockWidth:8,blockHeight:8,isCompressed:!0,channels:4},"astc-8x8-unorm-srgb":{bytesPerBlock:16,blockWidth:8,blockHeight:8,isCompressed:!0,channels:4},"astc-10x5-unorm":{bytesPerBlock:16,blockWidth:10,blockHeight:5,isCompressed:!0,channels:4},"astc-10x5-unorm-srgb":{bytesPerBlock:16,blockWidth:10,blockHeight:5,isCompressed:!0,channels:4},"astc-10x6-unorm":{bytesPerBlock:16,blockWidth:10,blockHeight:6,isCompressed:!0,channels:4},"astc-10x6-unorm-srgb":{bytesPerBlock:16,blockWidth:10,blockHeight:6,isCompressed:!0,channels:4},"astc-10x8-unorm":{bytesPerBlock:16,blockWidth:10,blockHeight:8,isCompressed:!0,channels:4},"astc-10x8-unorm-srgb":{bytesPerBlock:16,blockWidth:10,blockHeight:8,isCompressed:!0,channels:4},"astc-10x10-unorm":{bytesPerBlock:16,blockWidth:10,blockHeight:10,isCompressed:!0,channels:4},"astc-10x10-unorm-srgb":{bytesPerBlock:16,blockWidth:10,blockHeight:10,isCompressed:!0,channels:4},"astc-12x10-unorm":{bytesPerBlock:16,blockWidth:12,blockHeight:10,isCompressed:!0,channels:4},"astc-12x10-unorm-srgb":{bytesPerBlock:16,blockWidth:12,blockHeight:10,isCompressed:!0,channels:4},"astc-12x12-unorm":{bytesPerBlock:16,blockWidth:12,blockHeight:12,isCompressed:!0,channels:4},"astc-12x12-unorm-srgb":{bytesPerBlock:16,blockWidth:12,blockHeight:12,isCompressed:!0,channels:4}},z=class{constructor(){this.id=z._id++,this.line=0}get isAstNode(){return!0}get astNodeType(){return""}search(e){e(this)}searchBlock(e,t){if(e){t(Se.instance);for(let n of e)n instanceof Array?this.searchBlock(n,t):n.search(t);t(Ae.instance)}}constEvaluate(e,t){throw new Error("Cannot evaluate node")}constEvaluateString(e){return this.constEvaluate(e).toString()}};z._id=0;var Se=class extends z{};Se.instance=new Se;var Ae=class extends z{};Ae.instance=new Ae;var Zn=new Set(["all","all","any","select","arrayLength","abs","acos","acosh","asin","asinh","atan","atanh","atan2","ceil","clamp","cos","cosh","countLeadingZeros","countOneBits","countTrailingZeros","cross","degrees","determinant","distance","dot","dot4U8Packed","dot4I8Packed","exp","exp2","extractBits","faceForward","firstLeadingBit","firstTrailingBit","floor","fma","fract","frexp","insertBits","inverseSqrt","ldexp","length","log","log2","max","min","mix","modf","normalize","pow","quantizeToF16","radians","reflect","refract","reverseBits","round","saturate","sign","sin","sinh","smoothStep","sqrt","step","tan","tanh","transpose","trunc","dpdx","dpdxCoarse","dpdxFine","dpdy","dpdyCoarse","dpdyFine","fwidth","fwidthCoarse","fwidthFine","textureDimensions","textureGather","textureGatherCompare","textureLoad","textureNumLayers","textureNumLevels","textureNumSamples","textureSample","textureSampleBias","textureSampleCompare","textureSampleCompareLevel","textureSampleGrad","textureSampleLevel","textureSampleBaseClampToEdge","textureStore","atomicLoad","atomicStore","atomicAdd","atomicSub","atomicMax","atomicMin","atomicAnd","atomicOr","atomicXor","atomicExchange","atomicCompareExchangeWeak","pack4x8snorm","pack4x8unorm","pack4xI8","pack4xU8","pack4x8Clamp","pack4xU8Clamp","pack2x16snorm","pack2x16unorm","pack2x16float","unpack4x8snorm","unpack4x8unorm","unpack4xI8","unpack4xU8","unpack2x16snorm","unpack2x16unorm","unpack2x16float","storageBarrier","textureBarrier","workgroupBarrier","workgroupUniformLoad","subgroupAdd","subgroupExclusiveAdd","subgroupInclusiveAdd","subgroupAll","subgroupAnd","subgroupAny","subgroupBallot","subgroupBroadcast","subgroupBroadcastFirst","subgroupElect","subgroupMax","subgroupMin","subgroupMul","subgroupExclusiveMul","subgroupInclusiveMul","subgroupOr","subgroupShuffle","subgroupShuffleDown","subgroupShuffleUp","subgroupShuffleXor","subgroupXor","quadBroadcast","quadSwapDiagonal","quadSwapX","quadSwapY"]),F=class extends z{constructor(){super()}},ge=class extends F{constructor(e,t,n,r,s,i){super(),this.calls=new Set,this.name=e,this.args=t,this.returnType=n,this.body=r,this.startLine=s,this.endLine=i}get astNodeType(){return"function"}search(e){if(this.attributes)for(let t of this.attributes)e(t);e(this);for(let t of this.args)e(t);this.searchBlock(this.body,e)}},Yt=class extends F{constructor(e){super(),this.expression=e}get astNodeType(){return"staticAssert"}search(e){this.expression.search(e)}},at=class extends F{constructor(e,t){super(),this.condition=e,this.body=t}get astNodeType(){return"while"}search(e){this.condition.search(e),this.searchBlock(this.body,e)}},Be=class extends F{constructor(e,t){super(),this.body=e,this.loopId=t}get astNodeType(){return"continuing"}search(e){this.searchBlock(this.body,e)}},lt=class extends F{constructor(e,t,n,r){super(),this.init=e,this.condition=t,this.increment=n,this.body=r}get astNodeType(){return"for"}search(e){var t,n,r;(t=this.init)===null||t===void 0||t.search(e),(n=this.condition)===null||n===void 0||n.search(e),(r=this.increment)===null||r===void 0||r.search(e),this.searchBlock(this.body,e)}},ee=class extends F{constructor(e,t,n,r,s){super(),this.attributes=null,this.name=e,this.type=t,this.storage=n,this.access=r,this.value=s}get astNodeType(){return"var"}search(e){var t;e(this),(t=this.value)===null||t===void 0||t.search(e)}},Ve=class extends F{constructor(e,t,n){super(),this.attributes=null,this.name=e,this.type=t,this.value=n}get astNodeType(){return"override"}search(e){var t;(t=this.value)===null||t===void 0||t.search(e)}},me=class extends F{constructor(e,t,n,r,s){super(),this.attributes=null,this.name=e,this.type=t,this.storage=n,this.access=r,this.value=s}get astNodeType(){return"let"}search(e){var t;e(this),(t=this.value)===null||t===void 0||t.search(e)}},ke=class extends F{constructor(e,t,n,r,s){super(),this.attributes=null,this.name=e,this.type=t,this.storage=n,this.access=r,this.value=s}get astNodeType(){return"const"}constEvaluate(e,t){return this.value.constEvaluate(e,t)}search(e){var t;e(this),(t=this.value)===null||t===void 0||t.search(e)}},ye,Me,v,_;(a=>{a.increment="++",a.decrement="--"})(ye||(ye={})),(a=>{a.parse=function(e){let t=e;if(t=="parse")throw new Error("Invalid value for IncrementOperator");return a[t]}})(ye||(ye={}));var ct=class extends F{constructor(e,t){super(),this.operator=e,this.variable=t}get astNodeType(){return"increment"}search(e){this.variable.search(e)}};(a=>{a.assign="=",a.addAssign="+=",a.subtractAssin="-=",a.multiplyAssign="*=",a.divideAssign="/=",a.moduloAssign="%=",a.andAssign="&=",a.orAssign="|=",a.xorAssign="^=",a.shiftLeftAssign="<<=",a.shiftRightAssign=">>="})(Me||(Me={})),(a=>{a.parse=function(e){let t=e;if(t=="parse")throw new Error("Invalid value for AssignOperator");return t}})(Me||(Me={}));var ut=class extends F{constructor(e,t,n){super(),this.operator=e,this.variable=t,this.value=n}get astNodeType(){return"assign"}search(e){this.variable.search(e),this.value.search(e)}},Ue=class extends F{constructor(e,t){super(),this.name=e,this.args=t}get astNodeType(){return"call"}isBuiltin(){return Zn.has(this.name)}search(e){for(let t of this.args)t.search(e);e(this)}},ft=class extends F{constructor(e,t){super(),this.body=e,this.continuing=t}get astNodeType(){return"loop"}},ht=class extends F{constructor(e,t){super(),this.condition=e,this.cases=t}get astNodeType(){return"switch"}},pt=class extends F{constructor(e,t,n,r){super(),this.condition=e,this.body=t,this.elseif=n,this.else=r}get astNodeType(){return"if"}search(e){this.condition.search(e),this.searchBlock(this.body,e),this.searchBlock(this.elseif,e),this.searchBlock(this.else,e)}},dt=class extends F{constructor(e){super(),this.value=e}get astNodeType(){return"return"}search(e){var t;(t=this.value)===null||t===void 0||t.search(e)}},Jt=class extends F{constructor(e){super(),this.name=e}get astNodeType(){return"enable"}},en=class extends F{constructor(e){super(),this.extensions=e}get astNodeType(){return"requires"}},mt=class extends F{constructor(e,t){super(),this.severity=e,this.rule=t}get astNodeType(){return"diagnostic"}},$e=class extends F{constructor(e,t){super(),this.name=e,this.type=t}get astNodeType(){return"alias"}},tn=class extends F{constructor(){super()}get astNodeType(){return"discard"}},_t=class extends F{constructor(){super(),this.condition=null,this.loopId=-1}get astNodeType(){return"break"}},gt=class extends F{constructor(){super(),this.loopId=-1}get astNodeType(){return"continue"}},x=class extends F{constructor(e){super(),this.attributes=null,this.name=e}get astNodeType(){return"type"}get isStruct(){return!1}get isArray(){return!1}static maxFormatType(e){let t=e[0];if(t.name==="f32")return t;for(let n=1;n<e.length;++n){let r=x._priority.get(t.name);x._priority.get(e[n].name)<r&&(t=e[n])}return t.name==="x32"?x.i32:t}getTypeName(){return this.name}};x.x32=new x("x32"),x.f32=new x("f32"),x.i32=new x("i32"),x.u32=new x("u32"),x.f16=new x("f16"),x.bool=new x("bool"),x.void=new x("void"),x._priority=new Map([["f32",0],["f16",1],["u32",2],["i32",3],["x32",3]]);var vt=class extends x{constructor(e){super(e)}},Y=class extends x{constructor(e,t,n,r){super(e),this.members=t,this.startLine=n,this.endLine=r}get astNodeType(){return"struct"}get isStruct(){return!0}getMemberIndex(e){for(let t=0;t<this.members.length;t++)if(this.members[t].name==e)return t;return-1}search(e){for(let t of this.members)e(t)}},g=class extends x{constructor(e,t,n){super(e),this.format=t,this.access=n}get astNodeType(){return"template"}getTypeName(){let e=this.name;if(this.format!==null){if(e==="vec2"||e==="vec3"||e==="vec4"||e==="mat2x2"||e==="mat2x3"||e==="mat2x4"||e==="mat3x2"||e==="mat3x3"||e==="mat3x4"||e==="mat4x2"||e==="mat4x3"||e==="mat4x4"){if(this.format.name==="f32")return e+="f",e;if(this.format.name==="i32")return e+="i",e;if(this.format.name==="u32")return e+="u",e;if(this.format.name==="bool")return e+="b",e;if(this.format.name==="f16")return e+="h",e}e+=`<${this.format.name}>`}else if(e==="vec2"||e==="vec3"||e==="vec4")return e;return e}};g.vec2f=new g("vec2",x.f32,null),g.vec3f=new g("vec3",x.f32,null),g.vec4f=new g("vec4",x.f32,null),g.vec2i=new g("vec2",x.i32,null),g.vec3i=new g("vec3",x.i32,null),g.vec4i=new g("vec4",x.i32,null),g.vec2u=new g("vec2",x.u32,null),g.vec3u=new g("vec3",x.u32,null),g.vec4u=new g("vec4",x.u32,null),g.vec2h=new g("vec2",x.f16,null),g.vec3h=new g("vec3",x.f16,null),g.vec4h=new g("vec4",x.f16,null),g.vec2b=new g("vec2",x.bool,null),g.vec3b=new g("vec3",x.bool,null),g.vec4b=new g("vec4",x.bool,null),g.mat2x2f=new g("mat2x2",x.f32,null),g.mat2x3f=new g("mat2x3",x.f32,null),g.mat2x4f=new g("mat2x4",x.f32,null),g.mat3x2f=new g("mat3x2",x.f32,null),g.mat3x3f=new g("mat3x3",x.f32,null),g.mat3x4f=new g("mat3x4",x.f32,null),g.mat4x2f=new g("mat4x2",x.f32,null),g.mat4x3f=new g("mat4x3",x.f32,null),g.mat4x4f=new g("mat4x4",x.f32,null),g.mat2x2h=new g("mat2x2",x.f16,null),g.mat2x3h=new g("mat2x3",x.f16,null),g.mat2x4h=new g("mat2x4",x.f16,null),g.mat3x2h=new g("mat3x2",x.f16,null),g.mat3x3h=new g("mat3x3",x.f16,null),g.mat3x4h=new g("mat3x4",x.f16,null),g.mat4x2h=new g("mat4x2",x.f16,null),g.mat4x3h=new g("mat4x3",x.f16,null),g.mat4x4h=new g("mat4x4",x.f16,null),g.mat2x2i=new g("mat2x2",x.i32,null),g.mat2x3i=new g("mat2x3",x.i32,null),g.mat2x4i=new g("mat2x4",x.i32,null),g.mat3x2i=new g("mat3x2",x.i32,null),g.mat3x3i=new g("mat3x3",x.i32,null),g.mat3x4i=new g("mat3x4",x.i32,null),g.mat4x2i=new g("mat4x2",x.i32,null),g.mat4x3i=new g("mat4x3",x.i32,null),g.mat4x4i=new g("mat4x4",x.i32,null),g.mat2x2u=new g("mat2x2",x.u32,null),g.mat2x3u=new g("mat2x3",x.u32,null),g.mat2x4u=new g("mat2x4",x.u32,null),g.mat3x2u=new g("mat3x2",x.u32,null),g.mat3x3u=new g("mat3x3",x.u32,null),g.mat3x4u=new g("mat3x4",x.u32,null),g.mat4x2u=new g("mat4x2",x.u32,null),g.mat4x3u=new g("mat4x3",x.u32,null),g.mat4x4u=new g("mat4x4",x.u32,null);var Re=class extends x{constructor(e,t,n,r){super(e),this.storage=t,this.type=n,this.access=r}get astNodeType(){return"pointer"}},_e=class extends x{constructor(e,t,n,r){super(e),this.attributes=t,this.format=n,this.count=r}get astNodeType(){return"array"}get isArray(){return!0}},de=class extends x{constructor(e,t,n){super(e),this.format=t,this.access=n}get astNodeType(){return"sampler"}},Q=class extends z{constructor(){super(),this.postfix=null}},ae=class extends Q{constructor(e){super(),this.value=e}get astNodeType(){return"stringExpr"}toString(){return this.value}constEvaluateString(){return this.value}},K=class extends Q{constructor(e,t){super(),this.type=e,this.args=t}get astNodeType(){return"createExpr"}search(e){if(e(this),this.args)for(let t of this.args)t.search(e)}constEvaluate(e,t){return t&&(t[0]=this.type),e.evalExpression(this,e.context)}},He=class extends Q{constructor(e,t){super(),this.cachedReturnValue=null,this.name=e,this.args=t}get astNodeType(){return"callExpr"}setCachedReturnValue(e){this.cachedReturnValue=e}get isBuiltin(){return Zn.has(this.name)}constEvaluate(e,t){return e.evalExpression(this,e.context)}search(e){for(let t of this.args)t.search(e);e(this)}},G=class extends Q{constructor(e){super(),this.name=e}get astNodeType(){return"varExpr"}search(e){e(this),this.postfix&&this.postfix.search(e)}constEvaluate(e,t){return e.evalExpression(this,e.context)}},bt=class extends Q{constructor(e,t){super(),this.name=e,this.initializer=t}get astNodeType(){return"constExpr"}constEvaluate(e,t){if(this.initializer){let n=e.evalExpression(this.initializer,e.context);return n!==null&&this.postfix?n.getSubData(e,this.postfix,e.context):n}return null}search(e){this.initializer.search(e)}},R=class extends Q{constructor(e,t){super(),this.value=e,this.type=t}get astNodeType(){return"literalExpr"}constEvaluate(e,t){return t!==void 0&&(t[0]=this.type),this.value}get isScalar(){return this.value instanceof d}get isVector(){return this.value instanceof p||this.value instanceof I}get scalarValue(){return this.value instanceof d?this.value.value:(console.error("Value is not scalar."),0)}get vectorValue(){return this.value instanceof p||this.value instanceof I?this.value.data:(console.error("Value is not a vector or matrix."),new Float32Array(0))}},xt=class extends Q{constructor(e,t){super(),this.type=e,this.value=t}get astNodeType(){return"bitcastExpr"}search(e){this.value.search(e)}};var fe=class extends Q{constructor(e){super(),this.index=e}search(e){this.index.search(e)}},yt=class extends Q{constructor(){super()}},M=class extends yt{constructor(e,t){super(),this.operator=e,this.right=t}get astNodeType(){return"unaryOp"}constEvaluate(e,t){return e.evalExpression(this,e.context)}search(e){this.right.search(e)}},j=class extends yt{constructor(e,t,n){super(),this.operator=e,this.left=t,this.right=n}get astNodeType(){return"binaryOp"}_getPromotedType(e,t){return e.name===t.name?e:e.name==="f32"||t.name==="f32"?x.f32:e.name==="u32"||t.name==="u32"?x.u32:x.i32}constEvaluate(e,t){return e.evalExpression(this,e.context)}search(e){this.left.search(e),this.right.search(e)}},wt=class extends z{constructor(e){super(),this.body=e}},Ie=class extends Q{constructor(){super()}get astNodeType(){return"default"}},kt=class extends wt{constructor(e,t){super(t),this.selectors=e}get astNodeType(){return"case"}search(e){this.searchBlock(this.body,e)}},It=class extends wt{constructor(e){super(e)}get astNodeType(){return"default"}search(e){this.searchBlock(this.body,e)}},St=class extends z{constructor(e,t,n){super(),this.name=e,this.type=t,this.attributes=n}get astNodeType(){return"argument"}},nn=class extends z{constructor(e,t){super(),this.condition=e,this.body=t}get astNodeType(){return"elseif"}search(e){this.condition.search(e),this.searchBlock(this.body,e)}},At=class extends z{constructor(e,t,n){super(),this.name=e,this.type=t,this.attributes=n}get astNodeType(){return"member"}},Tt=class extends z{constructor(e,t){super(),this.name=e,this.value=t}get astNodeType(){return"attribute"}},q=class{constructor(e,t){this.parent=null,this.typeInfo=e,this.parent=t,this.id=q._id++}clone(){throw`Clone: Not implemented for ${this.constructor.name}`}setDataValue(e,t,n,r){console.error(`SetDataValue: Not implemented for ${this.constructor.name}`)}getSubData(e,t,n){return console.error(`GetDataValue: Not implemented for ${this.constructor.name}`),null}toString(){return`<${this.typeInfo.name}>`}};q._id=0;var Ge=class extends q{constructor(){super(new W("void",null),null)}toString(){return"void"}};Ge.void=new Ge;var ue=class extends q{constructor(e){super(new W("pointer",null),null),this.reference=e}clone(){return this}setDataValue(e,t,n,r){this.reference.setDataValue(e,t,n,r)}getSubData(e,t,n){return t?this.reference.getSubData(e,t,n):this}},d=class extends q{constructor(e,t,n=null){super(t,n),e instanceof Int32Array||e instanceof Uint32Array||e instanceof Float32Array?this.data=e:this.typeInfo.name==="x32"?e-Math.floor(e)!=0?this.data=new Float32Array([e]):this.data=e>=0?new Uint32Array([e]):new Int32Array([e]):this.typeInfo.name==="i32"||this.typeInfo.name==="bool"?this.data=new Int32Array([e]):this.typeInfo.name==="u32"?this.data=new Uint32Array([e]):this.typeInfo.name==="f32"||this.typeInfo.name==="f16"?this.data=new Float32Array([e]):console.error("ScalarData2: Invalid type",t)}clone(){if(this.data instanceof Float32Array)return new d(new Float32Array(this.data),this.typeInfo,null);if(this.data instanceof Int32Array)return new d(new Int32Array(this.data),this.typeInfo,null);if(this.data instanceof Uint32Array)return new d(new Uint32Array(this.data),this.typeInfo,null);throw"ScalarData: Invalid data type"}get value(){return this.data[0]}set value(e){this.data[0]=e}setDataValue(e,t,n,r){if(n)return void console.error("SetDataValue: Scalar data does not support postfix",n);if(!(t instanceof d))return void console.error("SetDataValue: Invalid value",t);let s=t.data[0];this.typeInfo.name==="i32"||this.typeInfo.name==="u32"?s=Math.floor(s):this.typeInfo.name==="bool"&&(s=s?1:0),this.data[0]=s}getSubData(e,t,n){return t?(console.error("getSubData: Scalar data does not support postfix",t),null):this}toString(){return`${this.value}`}};function Jr(a,e,t){let n=e.length;return n===2?t==="f32"?new p(new Float32Array(e),a.getTypeInfo("vec2f")):t==="i32"||t==="bool"?new p(new Int32Array(e),a.getTypeInfo("vec2i")):t==="u32"?new p(new Uint32Array(e),a.getTypeInfo("vec2u")):t==="f16"?new p(new Float32Array(e),a.getTypeInfo("vec2h")):(console.error(`getSubData: Unknown format ${t}`),null):n===3?t==="f32"?new p(new Float32Array(e),a.getTypeInfo("vec3f")):t==="i32"||t==="bool"?new p(new Int32Array(e),a.getTypeInfo("vec3i")):t==="u32"?new p(new Uint32Array(e),a.getTypeInfo("vec3u")):t==="f16"?new p(new Float32Array(e),a.getTypeInfo("vec3h")):(console.error(`getSubData: Unknown format ${t}`),null):n===4?t==="f32"?new p(new Float32Array(e),a.getTypeInfo("vec4f")):t==="i32"||t==="bool"?new p(new Int32Array(e),a.getTypeInfo("vec4i")):t==="u32"?new p(new Uint32Array(e),a.getTypeInfo("vec4u")):t==="f16"?new p(new Float32Array(e),a.getTypeInfo("vec4h")):(console.error(`getSubData: Unknown format ${t}`),null):(console.error(`getSubData: Invalid vector size ${e.length}`),null)}var p=class extends q{constructor(e,t,n=null){if(super(t,n),e instanceof Float32Array||e instanceof Uint32Array||e instanceof Int32Array)this.data=e;else{let r=this.typeInfo.name;r==="vec2f"||r==="vec3f"||r==="vec4f"?this.data=new Float32Array(e):r==="vec2i"||r==="vec3i"||r==="vec4i"?this.data=new Int32Array(e):r==="vec2u"||r==="vec3u"||r==="vec4u"?this.data=new Uint32Array(e):r==="vec2h"||r==="vec3h"||r==="vec4h"?this.data=new Float32Array(e):r==="vec2b"||r==="vec3b"||r==="vec4b"?this.data=new Int32Array(e):r==="vec2"||r==="vec3"||r==="vec4"?this.data=new Float32Array(e):console.error(`VectorData: Invalid type ${r}`)}}clone(){if(this.data instanceof Float32Array)return new p(new Float32Array(this.data),this.typeInfo,null);if(this.data instanceof Int32Array)return new p(new Int32Array(this.data),this.typeInfo,null);if(this.data instanceof Uint32Array)return new p(new Uint32Array(this.data),this.typeInfo,null);throw"VectorData: Invalid data type"}setDataValue(e,t,n,r){n instanceof ae?console.error("TODO: Set vector postfix"):t instanceof p?this.data=t.data:console.error("SetDataValue: Invalid value",t)}getSubData(e,t,n){if(t===null)return this;let r=e.getTypeInfo("f32");if(this.typeInfo instanceof ie)r=this.typeInfo.format||r;else{let i=this.typeInfo.name;i==="vec2f"||i==="vec3f"||i==="vec4f"?r=e.getTypeInfo("f32"):i==="vec2i"||i==="vec3i"||i==="vec4i"?r=e.getTypeInfo("i32"):i==="vec2b"||i==="vec3b"||i==="vec4b"?r=e.getTypeInfo("bool"):i==="vec2u"||i==="vec3u"||i==="vec4u"?r=e.getTypeInfo("u32"):i==="vec2h"||i==="vec3h"||i==="vec4h"?r=e.getTypeInfo("f16"):console.error(`GetSubData: Unknown type ${i}`)}let s=this;for(;t!==null&&s!==null;){if(t instanceof fe){let i=t.index,o=-1;if(i instanceof R){if(!(i.value instanceof d))return console.error(`GetSubData: Invalid array index ${i.value}`),null;o=i.value.value}else{let l=e.evalExpression(i,n);if(!(l instanceof d))return console.error("GetSubData: Unknown index type",i),null;o=l.value}if(o<0||o>=s.data.length)return console.error("GetSubData: Index out of range",o),null;if(s.data instanceof Float32Array){let l=new Float32Array(s.data.buffer,s.data.byteOffset+4*o,1);return new d(l,r)}if(s.data instanceof Int32Array){let l=new Int32Array(s.data.buffer,s.data.byteOffset+4*o,1);return new d(l,r)}if(s.data instanceof Uint32Array){let l=new Uint32Array(s.data.buffer,s.data.byteOffset+4*o,1);return new d(l,r)}throw"GetSubData: Invalid data type"}if(!(t instanceof ae))return console.error("GetSubData: Unknown postfix",t),null;{let i=t.value.toLowerCase();if(i.length===1){let l=0;if(i==="x"||i==="r")l=0;else if(i==="y"||i==="g")l=1;else if(i==="z"||i==="b")l=2;else{if(i!=="w"&&i!=="a")return console.error(`GetSubData: Unknown member ${i}`),null;l=3}if(this.data instanceof Float32Array){let c=new Float32Array(this.data.buffer,this.data.byteOffset+4*l,1);return new d(c,r,this)}if(this.data instanceof Int32Array){let c=new Int32Array(this.data.buffer,this.data.byteOffset+4*l,1);return new d(c,r,this)}if(this.data instanceof Uint32Array){let c=new Uint32Array(this.data.buffer,this.data.byteOffset+4*l,1);return new d(c,r,this)}}let o=[];for(let l of i)l==="x"||l==="r"?o.push(this.data[0]):l==="y"||l==="g"?o.push(this.data[1]):l==="z"||l==="b"?o.push(this.data[2]):l==="w"||l==="a"?o.push(this.data[3]):console.error(`GetDataValue: Unknown member ${l}`);s=Jr(e,o,r.name)}t=t.postfix}return s}toString(){let e=`${this.data[0]}`;for(let t=1;t<this.data.length;++t)e+=`, ${this.data[t]}`;return e}},I=class extends q{constructor(e,t,n=null){super(t,n),e instanceof Float32Array?this.data=e:this.data=new Float32Array(e)}clone(){return new I(new Float32Array(this.data),this.typeInfo,null)}setDataValue(e,t,n,r){n instanceof ae?console.error("TODO: Set matrix postfix"):t instanceof I?this.data=t.data:console.error("SetDataValue: Invalid value",t)}getSubData(e,t,n){if(t===null)return this;let r=this.typeInfo.name;if(e.getTypeInfo("f32"),this.typeInfo instanceof ie)this.typeInfo.format;else if(r.endsWith("f"))e.getTypeInfo("f32");else if(r.endsWith("i"))e.getTypeInfo("i32");else if(r.endsWith("u"))e.getTypeInfo("u32");else{if(!r.endsWith("h"))return console.error(`GetDataValue: Unknown type ${r}`),null;e.getTypeInfo("f16")}if(t instanceof fe){let s=t.index,i=-1;if(s instanceof R){if(!(s.value instanceof d))return console.error(`GetDataValue: Invalid array index ${s.value}`),null;i=s.value.value}else{let c=e.evalExpression(s,n);if(!(c instanceof d))return console.error("GetDataValue: Unknown index type",s),null;i=c.value}if(i<0||i>=this.data.length)return console.error("GetDataValue: Index out of range",i),null;let o=r.endsWith("h")?"h":"f",l;if(r==="mat2x2"||r==="mat2x2f"||r==="mat2x2h"||r==="mat3x2"||r==="mat3x2f"||r==="mat3x2h"||r==="mat4x2"||r==="mat4x2f"||r==="mat4x2h")l=new p(new Float32Array(this.data.buffer,this.data.byteOffset+2*i*4,2),e.getTypeInfo(`vec2${o}`));else if(r==="mat2x3"||r==="mat2x3f"||r==="mat2x3h"||r==="mat3x3"||r==="mat3x3f"||r==="mat3x3h"||r==="mat4x3"||r==="mat4x3f"||r==="mat4x3h")l=new p(new Float32Array(this.data.buffer,this.data.byteOffset+3*i*4,3),e.getTypeInfo(`vec3${o}`));else{if(r!=="mat2x4"&&r!=="mat2x4f"&&r!=="mat2x4h"&&r!=="mat3x4"&&r!=="mat3x4f"&&r!=="mat3x4h"&&r!=="mat4x4"&&r!=="mat4x4f"&&r!=="mat4x4h")return console.error(`GetDataValue: Unknown type ${r}`),null;l=new p(new Float32Array(this.data.buffer,this.data.byteOffset+4*i*4,4),e.getTypeInfo(`vec4${o}`))}return t.postfix?l.getSubData(e,t.postfix,n):l}return console.error("GetDataValue: Invalid postfix",t),null}toString(){let e=`${this.data[0]}`;for(let t=1;t<this.data.length;++t)e+=`, ${this.data[t]}`;return e}},D=class extends q{constructor(e,t,n=0,r=null){super(t,r),this.buffer=e instanceof ArrayBuffer?e:e.buffer,this.offset=n}clone(){let e=new Uint8Array(new Uint8Array(this.buffer,this.offset,this.typeInfo.size));return new D(e.buffer,this.typeInfo,0,null)}setDataValue(e,t,n,r){if(t===null)return void console.log("setDataValue: NULL data.");let s=this.offset,i=this.typeInfo;for(;n;){if(n instanceof fe)if(i instanceof se){let o=n.index;if(o instanceof R){if(!(o.value instanceof d))return void console.error(`SetDataValue: Invalid index type ${o.value}`);s+=o.value.value*i.stride}else{let l=e.evalExpression(o,r);if(!(l instanceof d))return void console.error("SetDataValue: Unknown index type",o);s+=l.value*i.stride}i=i.format}else console.error(`SetDataValue: Type ${i.getTypeName()} is not an array`);else{if(!(n instanceof ae))return void console.error("SetDataValue: Unknown postfix type",n);{let o=n.value;if(i instanceof re){let l=!1;for(let c of i.members)if(c.name===o){s+=c.offset,i=c.type,l=!0;break}if(!l)return void console.error(`SetDataValue: Member ${o} not found`)}else if(i instanceof W){let l=i.getTypeName(),c=0;if(o==="x"||o==="r")c=0;else if(o==="y"||o==="g")c=1;else if(o==="z"||o==="b")c=2;else{if(o!=="w"&&o!=="a")return void console.error(`SetDataValue: Unknown member ${o}`);c=3}if(!(t instanceof d))return void console.error("SetDataValue: Invalid value",t);let u=t.value;return l==="vec2f"?void(new Float32Array(this.buffer,s,2)[c]=u):l==="vec3f"?void(new Float32Array(this.buffer,s,3)[c]=u):l==="vec4f"?void(new Float32Array(this.buffer,s,4)[c]=u):l==="vec2i"?void(new Int32Array(this.buffer,s,2)[c]=u):l==="vec3i"?void(new Int32Array(this.buffer,s,3)[c]=u):l==="vec4i"?void(new Int32Array(this.buffer,s,4)[c]=u):l==="vec2u"?void(new Uint32Array(this.buffer,s,2)[c]=u):l==="vec3u"?void(new Uint32Array(this.buffer,s,3)[c]=u):l==="vec4u"?void(new Uint32Array(this.buffer,s,4)[c]=u):void console.error(`SetDataValue: Type ${l} is not a struct`)}}}n=n.postfix}this.setData(e,t,i,s,r)}setData(e,t,n,r,s){let i=n.getTypeName();if(i!=="f32"&&i!=="f16")if(i!=="i32"&&i!=="atomic<i32>"&&i!=="x32")if(i!=="u32"&&i!=="atomic<u32>")if(i!=="bool")if(i!=="vec2f"&&i!=="vec2h")if(i!=="vec3f"&&i!=="vec3h")if(i!=="vec4f"&&i!=="vec4h")if(i!=="vec2i")if(i!=="vec3i")if(i!=="vec4i")if(i!=="vec2u")if(i!=="vec3u")if(i!=="vec4u")if(i!=="vec2b")if(i!=="vec3b")if(i!=="vec4b")if(i!=="mat2x2f"&&i!=="mat2x2h")if(i!=="mat2x3f"&&i!=="mat2x3h")if(i!=="mat2x4f"&&i!=="mat2x4h")if(i!=="mat3x2f"&&i!=="mat3x2h")if(i!=="mat3x3f"&&i!=="mat3x3h")if(i!=="mat3x4f"&&i!=="mat3x4h")if(i!=="mat4x2f"&&i!=="mat4x2h")if(i!=="mat4x3f"&&i!=="mat4x3h")if(i!=="mat4x4f"&&i!=="mat4x4h")if(t instanceof D){if(n===t.typeInfo)return void new Uint8Array(this.buffer,r,t.buffer.byteLength).set(new Uint8Array(t.buffer));console.error("SetDataValue: Type mismatch",i,t.typeInfo.getTypeName())}else console.error(`SetData: Unknown type ${i}`);else{let o=new Float32Array(this.buffer,r,16);t instanceof I?(o[0]=t.data[0],o[1]=t.data[1],o[2]=t.data[2],o[3]=t.data[3],o[4]=t.data[4],o[5]=t.data[5],o[6]=t.data[6],o[7]=t.data[7],o[8]=t.data[8],o[9]=t.data[9],o[10]=t.data[10],o[11]=t.data[11],o[12]=t.data[12],o[13]=t.data[13],o[14]=t.data[14],o[15]=t.data[15]):(o[0]=t[0],o[1]=t[1],o[2]=t[2],o[3]=t[3],o[4]=t[4],o[5]=t[5],o[6]=t[6],o[7]=t[7],o[8]=t[8],o[9]=t[9],o[10]=t[10],o[11]=t[11],o[12]=t[12],o[13]=t[13],o[14]=t[14],o[15]=t[15])}else{let o=new Float32Array(this.buffer,r,12);t instanceof I?(o[0]=t.data[0],o[1]=t.data[1],o[2]=t.data[2],o[3]=t.data[3],o[4]=t.data[4],o[5]=t.data[5],o[6]=t.data[6],o[7]=t.data[7],o[8]=t.data[8],o[9]=t.data[9],o[10]=t.data[10],o[11]=t.data[11]):(o[0]=t[0],o[1]=t[1],o[2]=t[2],o[3]=t[3],o[4]=t[4],o[5]=t[5],o[6]=t[6],o[7]=t[7],o[8]=t[8],o[9]=t[9],o[10]=t[10],o[11]=t[11])}else{let o=new Float32Array(this.buffer,r,8);t instanceof I?(o[0]=t.data[0],o[1]=t.data[1],o[2]=t.data[2],o[3]=t.data[3],o[4]=t.data[4],o[5]=t.data[5],o[6]=t.data[6],o[7]=t.data[7]):(o[0]=t[0],o[1]=t[1],o[2]=t[2],o[3]=t[3],o[4]=t[4],o[5]=t[5],o[6]=t[6],o[7]=t[7])}else{let o=new Float32Array(this.buffer,r,12);t instanceof I?(o[0]=t.data[0],o[1]=t.data[1],o[2]=t.data[2],o[3]=t.data[3],o[4]=t.data[4],o[5]=t.data[5],o[6]=t.data[6],o[7]=t.data[7],o[8]=t.data[8],o[9]=t.data[9],o[10]=t.data[10],o[11]=t.data[11]):(o[0]=t[0],o[1]=t[1],o[2]=t[2],o[3]=t[3],o[4]=t[4],o[5]=t[5],o[6]=t[6],o[7]=t[7],o[8]=t[8],o[9]=t[9],o[10]=t[10],o[11]=t[11])}else{let o=new Float32Array(this.buffer,r,9);t instanceof I?(o[0]=t.data[0],o[1]=t.data[1],o[2]=t.data[2],o[3]=t.data[3],o[4]=t.data[4],o[5]=t.data[5],o[6]=t.data[6],o[7]=t.data[7],o[8]=t.data[8]):(o[0]=t[0],o[1]=t[1],o[2]=t[2],o[3]=t[3],o[4]=t[4],o[5]=t[5],o[6]=t[6],o[7]=t[7],o[8]=t[8])}else{let o=new Float32Array(this.buffer,r,6);t instanceof I?(o[0]=t.data[0],o[1]=t.data[1],o[2]=t.data[2],o[3]=t.data[3],o[4]=t.data[4],o[5]=t.data[5]):(o[0]=t[0],o[1]=t[1],o[2]=t[2],o[3]=t[3],o[4]=t[4],o[5]=t[5])}else{let o=new Float32Array(this.buffer,r,8);t instanceof I?(o[0]=t.data[0],o[1]=t.data[1],o[2]=t.data[2],o[3]=t.data[3],o[4]=t.data[4],o[5]=t.data[5],o[6]=t.data[6],o[7]=t.data[7]):(o[0]=t[0],o[1]=t[1],o[2]=t[2],o[3]=t[3],o[4]=t[4],o[5]=t[5],o[6]=t[6],o[7]=t[7])}else{let o=new Float32Array(this.buffer,r,6);t instanceof I?(o[0]=t.data[0],o[1]=t.data[1],o[2]=t.data[2],o[3]=t.data[3],o[4]=t.data[4],o[5]=t.data[5]):(o[0]=t[0],o[1]=t[1],o[2]=t[2],o[3]=t[3],o[4]=t[4],o[5]=t[5])}else{let o=new Float32Array(this.buffer,r,4);t instanceof I?(o[0]=t.data[0],o[1]=t.data[1],o[2]=t.data[2],o[3]=t.data[3]):(o[0]=t[0],o[1]=t[1],o[2]=t[2],o[3]=t[3])}else{let o=new Uint32Array(this.buffer,r,4);t instanceof p?(o[0]=t.data[0],o[1]=t.data[1],o[2]=t.data[2],o[3]=t.data[3]):(o[0]=t[0],o[1]=t[1],o[2]=t[2],o[3]=t[3])}else{let o=new Uint32Array(this.buffer,r,3);t instanceof p?(o[0]=t.data[0],o[1]=t.data[1],o[2]=t.data[2]):(o[0]=t[0],o[1]=t[1],o[2]=t[2])}else{let o=new Uint32Array(this.buffer,r,2);t instanceof p?(o[0]=t.data[0],o[1]=t.data[1]):(o[0]=t[0],o[1]=t[1])}else{let o=new Uint32Array(this.buffer,r,4);t instanceof p?(o[0]=t.data[0],o[1]=t.data[1],o[2]=t.data[2],o[3]=t.data[3]):(o[0]=t[0],o[1]=t[1],o[2]=t[2],o[3]=t[3])}else{let o=new Uint32Array(this.buffer,r,3);t instanceof p?(o[0]=t.data[0],o[1]=t.data[1],o[2]=t.data[2]):(o[0]=t[0],o[1]=t[1],o[2]=t[2])}else{let o=new Uint32Array(this.buffer,r,2);t instanceof p?(o[0]=t.data[0],o[1]=t.data[1]):(o[0]=t[0],o[1]=t[1])}else{let o=new Int32Array(this.buffer,r,4);t instanceof p?(o[0]=t.data[0],o[1]=t.data[1],o[2]=t.data[2],o[3]=t.data[3]):(o[0]=t[0],o[1]=t[1],o[2]=t[2],o[3]=t[3])}else{let o=new Int32Array(this.buffer,r,3);t instanceof p?(o[0]=t.data[0],o[1]=t.data[1],o[2]=t.data[2]):(o[0]=t[0],o[1]=t[1],o[2]=t[2])}else{let o=new Int32Array(this.buffer,r,2);t instanceof p?(o[0]=t.data[0],o[1]=t.data[1]):(o[0]=t[0],o[1]=t[1])}else{let o=new Float32Array(this.buffer,r,4);t instanceof p?(o[0]=t.data[0],o[1]=t.data[1],o[2]=t.data[2],o[3]=t.data[3]):(o[0]=t[0],o[1]=t[1],o[2]=t[2],o[3]=t[3])}else{let o=new Float32Array(this.buffer,r,3);t instanceof p?(o[0]=t.data[0],o[1]=t.data[1],o[2]=t.data[2]):(o[0]=t[0],o[1]=t[1],o[2]=t[2])}else{let o=new Float32Array(this.buffer,r,2);t instanceof p?(o[0]=t.data[0],o[1]=t.data[1]):(o[0]=t[0],o[1]=t[1])}else t instanceof d&&(new Int32Array(this.buffer,r,1)[0]=t.value);else t instanceof d&&(new Uint32Array(this.buffer,r,1)[0]=t.value);else t instanceof d&&(new Int32Array(this.buffer,r,1)[0]=t.value);else t instanceof d&&(new Float32Array(this.buffer,r,1)[0]=t.value)}getSubData(e,t,n){var r,s,i;if(t===null)return this;let o=this.offset,l=this.typeInfo;for(;t;){if(t instanceof fe){let u=t.index,h=e.evalExpression(u,n),b=0;if(h instanceof d?b=h.value:console.error("GetDataValue: Invalid index type",u),l instanceof se)o+=b*l.stride,l=l.format;else{let y=l.getTypeName();y==="mat4x4"||y==="mat4x4f"||y==="mat4x4h"?(o+=16*b,l=e.getTypeInfo("vec4f")):console.error(`getDataValue: Type ${l.getTypeName()} is not an array`)}}else{if(!(t instanceof ae))return console.error("GetDataValue: Unknown postfix type",t),null;{let u=t.value;if(l instanceof re){let h=!1;for(let b of l.members)if(b.name===u){o+=b.offset,l=b.type,h=!0;break}if(!h)return console.error(`GetDataValue: Member ${u} not found`),null}else if(l instanceof W){let h=l.getTypeName();if(h==="vec2f"||h==="vec3f"||h==="vec4f"||h==="vec2i"||h==="vec3i"||h==="vec4i"||h==="vec2u"||h==="vec3u"||h==="vec4u"||h==="vec2b"||h==="vec3b"||h==="vec4b"||h==="vec2h"||h==="vec3h"||h==="vec4h"||h==="vec2"||h==="vec3"||h==="vec4"){if(u.length>0&&u.length<5){let b="f",y=[];for(let S=0;S<u.length;++S){let E=u[S].toLowerCase(),T=0;if(E==="x"||E==="r")T=0;else if(E==="y"||E==="g")T=1;else if(E==="z"||E==="b")T=2;else{if(E!=="w"&&E!=="a")return console.error(`Unknown member ${u}`),null;T=3}if(u.length===1){if(h.endsWith("f"))return this.buffer.byteLength<o+4*T+4?(console.log("Insufficient buffer data"),null):new d(new Float32Array(this.buffer,o+4*T,1),e.getTypeInfo("f32"),this);if(h.endsWith("h"))return new d(new Float32Array(this.buffer,o+4*T,1),e.getTypeInfo("f16"),this);if(h.endsWith("i"))return new d(new Int32Array(this.buffer,o+4*T,1),e.getTypeInfo("i32"),this);if(h.endsWith("b"))return new d(new Int32Array(this.buffer,o+4*T,1),e.getTypeInfo("bool"),this);if(h.endsWith("u"))return new d(new Uint32Array(this.buffer,o+4*T,1),e.getTypeInfo("i32"),this)}if(h==="vec2f")y.push(new Float32Array(this.buffer,o,2)[T]);else if(h==="vec3f"){if(o+12>=this.buffer.byteLength)return console.log("Insufficient buffer data"),null;let L=new Float32Array(this.buffer,o,3);y.push(L[T])}else if(h==="vec4f")y.push(new Float32Array(this.buffer,o,4)[T]);else if(h==="vec2i")b="i",y.push(new Int32Array(this.buffer,o,2)[T]);else if(h==="vec3i")b="i",y.push(new Int32Array(this.buffer,o,3)[T]);else if(h==="vec4i")b="i",y.push(new Int32Array(this.buffer,o,4)[T]);else if(h==="vec2u"){b="u";let L=new Uint32Array(this.buffer,o,2);y.push(L[T])}else h==="vec3u"?(b="u",y.push(new Uint32Array(this.buffer,o,3)[T])):h==="vec4u"&&(b="u",y.push(new Uint32Array(this.buffer,o,4)[T]))}return y.length===2?l=e.getTypeInfo(`vec2${b}`):y.length===3?l=e.getTypeInfo(`vec3${b}`):y.length===4?l=e.getTypeInfo(`vec4${b}`):console.error(`GetDataValue: Invalid vector length ${y.length}`),new p(y,l,null)}return console.error(`GetDataValue: Unknown member ${u}`),null}return console.error(`GetDataValue: Type ${h} is not a struct`),null}}}t=t.postfix}let c=l.getTypeName();return c==="f32"?new d(new Float32Array(this.buffer,o,1),l,this):c==="i32"?new d(new Int32Array(this.buffer,o,1),l,this):c==="u32"?new d(new Uint32Array(this.buffer,o,1),l,this):c==="vec2f"?new p(new Float32Array(this.buffer,o,2),l,this):c==="vec3f"?new p(new Float32Array(this.buffer,o,3),l,this):c==="vec4f"?new p(new Float32Array(this.buffer,o,4),l,this):c==="vec2i"?new p(new Int32Array(this.buffer,o,2),l,this):c==="vec3i"?new p(new Int32Array(this.buffer,o,3),l,this):c==="vec4i"?new p(new Int32Array(this.buffer,o,4),l,this):c==="vec2u"?new p(new Uint32Array(this.buffer,o,2),l,this):c==="vec3u"?new p(new Uint32Array(this.buffer,o,3),l,this):c==="vec4u"?new p(new Uint32Array(this.buffer,o,4),l,this):l instanceof ie&&l.name==="atomic"?((r=l.format)===null||r===void 0?void 0:r.name)==="u32"?new d(new Uint32Array(this.buffer,o,1)[0],l.format,this):((s=l.format)===null||s===void 0?void 0:s.name)==="i32"?new d(new Int32Array(this.buffer,o,1)[0],l.format,this):(console.error(`GetDataValue: Invalid atomic format ${(i=l.format)===null||i===void 0?void 0:i.name}`),null):new D(this.buffer,l,o,this)}toString(){let e="";if(this.typeInfo instanceof se)if(this.typeInfo.format.name==="f32"){let t=new Float32Array(this.buffer,this.offset);e=`[${t[0]}`;for(let n=1;n<t.length;++n)e+=`, ${t[n]}`}else if(this.typeInfo.format.name==="i32"){let t=new Int32Array(this.buffer,this.offset);e=`[${t[0]}`;for(let n=1;n<t.length;++n)e+=`, ${t[n]}`}else if(this.typeInfo.format.name==="u32"){let t=new Uint32Array(this.buffer,this.offset);e=`[${t[0]}`;for(let n=1;n<t.length;++n)e+=`, ${t[n]}`}else if(this.typeInfo.format.name==="vec2f"){let t=new Float32Array(this.buffer,this.offset);e=`[${t[0]}, ${t[1]}]`;for(let n=1;n<t.length/2;++n)e+=`, [${t[2*n]}, ${t[2*n+1]}]`}else if(this.typeInfo.format.name==="vec3f"){let t=new Float32Array(this.buffer,this.offset);e=`[${t[0]}, ${t[1]}, ${t[2]}]`;for(let n=4;n<t.length;n+=4)e+=`, [${t[n]}, ${t[n+1]}, ${t[n+2]}]`}else if(this.typeInfo.format.name==="vec4f"){let t=new Float32Array(this.buffer,this.offset);e=`[${t[0]}, ${t[1]}, ${t[2]}, ${t[3]}]`;for(let n=4;n<t.length;n+=4)e+=`, [${t[n]}, ${t[n+1]}, ${t[n+2]}, ${t[n+3]}]`}else e="[...]";else this.typeInfo instanceof re?e+="{...}":e="[...]";return e}},J=class extends q{constructor(e,t,n,r){super(t,null),this.data=e,this.descriptor=n,this.view=r}clone(){return new J(this.data,this.typeInfo,this.descriptor,this.view)}get width(){var e,t;let n=this.descriptor.size;return n instanceof Array&&n.length>0?(e=n[0])!==null&&e!==void 0?e:0:n instanceof Object&&(t=n.width)!==null&&t!==void 0?t:0}get height(){var e,t;let n=this.descriptor.size;return n instanceof Array&&n.length>1?(e=n[1])!==null&&e!==void 0?e:0:n instanceof Object&&(t=n.height)!==null&&t!==void 0?t:0}get depthOrArrayLayers(){var e,t;let n=this.descriptor.size;return n instanceof Array&&n.length>2?(e=n[2])!==null&&e!==void 0?e:0:n instanceof Object&&(t=n.depthOrArrayLayers)!==null&&t!==void 0?t:0}get format(){var e;return this.descriptor&&(e=this.descriptor.format)!==null&&e!==void 0?e:"rgba8unorm"}get sampleCount(){var e;return this.descriptor&&(e=this.descriptor.sampleCount)!==null&&e!==void 0?e:1}get mipLevelCount(){var e;return this.descriptor&&(e=this.descriptor.mipLevelCount)!==null&&e!==void 0?e:1}get dimension(){var e;return this.descriptor&&(e=this.descriptor.dimension)!==null&&e!==void 0?e:"2d"}getMipLevelSize(e){if(e>=this.mipLevelCount)return[0,0,0];let t=[this.width,this.height,this.depthOrArrayLayers];for(let n=0;n<t.length;++n)t[n]=Math.max(1,t[n]>>e);return t}get texelByteSize(){let e=this.format,t=Wt[e];return t?t.isDepthStencil?4:t.bytesPerBlock:0}get bytesPerRow(){return this.width*this.texelByteSize}get isDepthStencil(){let e=this.format,t=Wt[e];return!!t&&t.isDepthStencil}getGpuSize(){let e=this.format,t=Wt[e],n=this.width;if(!e||n<=0||!t)return-1;let r=this.height,s=this.depthOrArrayLayers,i=this.dimension;return n/t.blockWidth*(i==="1d"?1:r/t.blockHeight)*t.bytesPerBlock*s}getPixel(e,t,n=0,r=0){let s=this.texelByteSize,i=this.bytesPerRow,o=this.height,l=this.data[r];return Yr(new Uint8Array(l),e,t,n,r,o,i,s,this.format)}setPixel(e,t,n,r,s){let i=this.texelByteSize,o=this.bytesPerRow,l=this.height,c=this.data[r];(function(u,h,b,y,S,E,T,L,P,k){let A=y*(T>>=S)*(E>>=S)+b*T+h*L;switch(P){case"r8unorm":return void N(u,A,"8unorm",1,k);case"r8snorm":return void N(u,A,"8snorm",1,k);case"r8uint":return void N(u,A,"8uint",1,k);case"r8sint":return void N(u,A,"8sint",1,k);case"rg8unorm":return void N(u,A,"8unorm",2,k);case"rg8snorm":return void N(u,A,"8snorm",2,k);case"rg8uint":return void N(u,A,"8uint",2,k);case"rg8sint":return void N(u,A,"8sint",2,k);case"rgba8unorm-srgb":case"rgba8unorm":case"bgra8unorm-srgb":case"bgra8unorm":return void N(u,A,"8unorm",4,k);case"rgba8snorm":return void N(u,A,"8snorm",4,k);case"rgba8uint":return void N(u,A,"8uint",4,k);case"rgba8sint":return void N(u,A,"8sint",4,k);case"r16uint":return void N(u,A,"16uint",1,k);case"r16sint":return void N(u,A,"16sint",1,k);case"r16float":return void N(u,A,"16float",1,k);case"rg16uint":return void N(u,A,"16uint",2,k);case"rg16sint":return void N(u,A,"16sint",2,k);case"rg16float":return void N(u,A,"16float",2,k);case"rgba16uint":return void N(u,A,"16uint",4,k);case"rgba16sint":return void N(u,A,"16sint",4,k);case"rgba16float":return void N(u,A,"16float",4,k);case"r32uint":return void N(u,A,"32uint",1,k);case"r32sint":return void N(u,A,"32sint",1,k);case"depth16unorm":case"depth24plus":case"depth24plus-stencil8":case"depth32float":case"depth32float-stencil8":case"r32float":return void N(u,A,"32float",1,k);case"rg32uint":return void N(u,A,"32uint",2,k);case"rg32sint":return void N(u,A,"32sint",2,k);case"rg32float":return void N(u,A,"32float",2,k);case"rgba32uint":return void N(u,A,"32uint",4,k);case"rgba32sint":return void N(u,A,"32sint",4,k);case"rgba32float":return void N(u,A,"32float",4,k);case"rg11b10ufloat":console.error("TODO: rg11b10ufloat not supported for writing")}})(new Uint8Array(c),e,t,n,r,l,o,i,this.format,s)}};(a=>{a[a.token=0]="token",a[a.keyword=1]="keyword",a[a.reserved=2]="reserved"})(_||(_={}));var m=class{constructor(e,t,n){this.name=e,this.type=t,this.rule=n}toString(){return this.name}},f=class{};v=f,f.none=new m("",_.reserved,""),f.eof=new m("EOF",_.token,""),f.reserved={asm:new m("asm",_.reserved,"asm"),bf16:new m("bf16",_.reserved,"bf16"),do:new m("do",_.reserved,"do"),enum:new m("enum",_.reserved,"enum"),f16:new m("f16",_.reserved,"f16"),f64:new m("f64",_.reserved,"f64"),handle:new m("handle",_.reserved,"handle"),i8:new m("i8",_.reserved,"i8"),i16:new m("i16",_.reserved,"i16"),i64:new m("i64",_.reserved,"i64"),mat:new m("mat",_.reserved,"mat"),premerge:new m("premerge",_.reserved,"premerge"),regardless:new m("regardless",_.reserved,"regardless"),typedef:new m("typedef",_.reserved,"typedef"),u8:new m("u8",_.reserved,"u8"),u16:new m("u16",_.reserved,"u16"),u64:new m("u64",_.reserved,"u64"),unless:new m("unless",_.reserved,"unless"),using:new m("using",_.reserved,"using"),vec:new m("vec",_.reserved,"vec"),void:new m("void",_.reserved,"void")},f.keywords={array:new m("array",_.keyword,"array"),atomic:new m("atomic",_.keyword,"atomic"),bool:new m("bool",_.keyword,"bool"),f32:new m("f32",_.keyword,"f32"),i32:new m("i32",_.keyword,"i32"),mat2x2:new m("mat2x2",_.keyword,"mat2x2"),mat2x3:new m("mat2x3",_.keyword,"mat2x3"),mat2x4:new m("mat2x4",_.keyword,"mat2x4"),mat3x2:new m("mat3x2",_.keyword,"mat3x2"),mat3x3:new m("mat3x3",_.keyword,"mat3x3"),mat3x4:new m("mat3x4",_.keyword,"mat3x4"),mat4x2:new m("mat4x2",_.keyword,"mat4x2"),mat4x3:new m("mat4x3",_.keyword,"mat4x3"),mat4x4:new m("mat4x4",_.keyword,"mat4x4"),ptr:new m("ptr",_.keyword,"ptr"),sampler:new m("sampler",_.keyword,"sampler"),sampler_comparison:new m("sampler_comparison",_.keyword,"sampler_comparison"),struct:new m("struct",_.keyword,"struct"),texture_1d:new m("texture_1d",_.keyword,"texture_1d"),texture_2d:new m("texture_2d",_.keyword,"texture_2d"),texture_2d_array:new m("texture_2d_array",_.keyword,"texture_2d_array"),texture_3d:new m("texture_3d",_.keyword,"texture_3d"),texture_cube:new m("texture_cube",_.keyword,"texture_cube"),texture_cube_array:new m("texture_cube_array",_.keyword,"texture_cube_array"),texture_multisampled_2d:new m("texture_multisampled_2d",_.keyword,"texture_multisampled_2d"),texture_storage_1d:new m("texture_storage_1d",_.keyword,"texture_storage_1d"),texture_storage_2d:new m("texture_storage_2d",_.keyword,"texture_storage_2d"),texture_storage_2d_array:new m("texture_storage_2d_array",_.keyword,"texture_storage_2d_array"),texture_storage_3d:new m("texture_storage_3d",_.keyword,"texture_storage_3d"),texture_depth_2d:new m("texture_depth_2d",_.keyword,"texture_depth_2d"),texture_depth_2d_array:new m("texture_depth_2d_array",_.keyword,"texture_depth_2d_array"),texture_depth_cube:new m("texture_depth_cube",_.keyword,"texture_depth_cube"),texture_depth_cube_array:new m("texture_depth_cube_array",_.keyword,"texture_depth_cube_array"),texture_depth_multisampled_2d:new m("texture_depth_multisampled_2d",_.keyword,"texture_depth_multisampled_2d"),texture_external:new m("texture_external",_.keyword,"texture_external"),u32:new m("u32",_.keyword,"u32"),vec2:new m("vec2",_.keyword,"vec2"),vec3:new m("vec3",_.keyword,"vec3"),vec4:new m("vec4",_.keyword,"vec4"),bitcast:new m("bitcast",_.keyword,"bitcast"),block:new m("block",_.keyword,"block"),break:new m("break",_.keyword,"break"),case:new m("case",_.keyword,"case"),continue:new m("continue",_.keyword,"continue"),continuing:new m("continuing",_.keyword,"continuing"),default:new m("default",_.keyword,"default"),diagnostic:new m("diagnostic",_.keyword,"diagnostic"),discard:new m("discard",_.keyword,"discard"),else:new m("else",_.keyword,"else"),enable:new m("enable",_.keyword,"enable"),fallthrough:new m("fallthrough",_.keyword,"fallthrough"),false:new m("false",_.keyword,"false"),fn:new m("fn",_.keyword,"fn"),for:new m("for",_.keyword,"for"),function:new m("function",_.keyword,"function"),if:new m("if",_.keyword,"if"),let:new m("let",_.keyword,"let"),const:new m("const",_.keyword,"const"),loop:new m("loop",_.keyword,"loop"),while:new m("while",_.keyword,"while"),private:new m("private",_.keyword,"private"),read:new m("read",_.keyword,"read"),read_write:new m("read_write",_.keyword,"read_write"),return:new m("return",_.keyword,"return"),requires:new m("requires",_.keyword,"requires"),storage:new m("storage",_.keyword,"storage"),switch:new m("switch",_.keyword,"switch"),true:new m("true",_.keyword,"true"),alias:new m("alias",_.keyword,"alias"),type:new m("type",_.keyword,"type"),uniform:new m("uniform",_.keyword,"uniform"),var:new m("var",_.keyword,"var"),override:new m("override",_.keyword,"override"),workgroup:new m("workgroup",_.keyword,"workgroup"),write:new m("write",_.keyword,"write"),r8unorm:new m("r8unorm",_.keyword,"r8unorm"),r8snorm:new m("r8snorm",_.keyword,"r8snorm"),r8uint:new m("r8uint",_.keyword,"r8uint"),r8sint:new m("r8sint",_.keyword,"r8sint"),r16uint:new m("r16uint",_.keyword,"r16uint"),r16sint:new m("r16sint",_.keyword,"r16sint"),r16float:new m("r16float",_.keyword,"r16float"),rg8unorm:new m("rg8unorm",_.keyword,"rg8unorm"),rg8snorm:new m("rg8snorm",_.keyword,"rg8snorm"),rg8uint:new m("rg8uint",_.keyword,"rg8uint"),rg8sint:new m("rg8sint",_.keyword,"rg8sint"),r32uint:new m("r32uint",_.keyword,"r32uint"),r32sint:new m("r32sint",_.keyword,"r32sint"),r32float:new m("r32float",_.keyword,"r32float"),rg16uint:new m("rg16uint",_.keyword,"rg16uint"),rg16sint:new m("rg16sint",_.keyword,"rg16sint"),rg16float:new m("rg16float",_.keyword,"rg16float"),rgba8unorm:new m("rgba8unorm",_.keyword,"rgba8unorm"),rgba8unorm_srgb:new m("rgba8unorm_srgb",_.keyword,"rgba8unorm_srgb"),rgba8snorm:new m("rgba8snorm",_.keyword,"rgba8snorm"),rgba8uint:new m("rgba8uint",_.keyword,"rgba8uint"),rgba8sint:new m("rgba8sint",_.keyword,"rgba8sint"),bgra8unorm:new m("bgra8unorm",_.keyword,"bgra8unorm"),bgra8unorm_srgb:new m("bgra8unorm_srgb",_.keyword,"bgra8unorm_srgb"),rgb10a2unorm:new m("rgb10a2unorm",_.keyword,"rgb10a2unorm"),rg11b10float:new m("rg11b10float",_.keyword,"rg11b10float"),rg32uint:new m("rg32uint",_.keyword,"rg32uint"),rg32sint:new m("rg32sint",_.keyword,"rg32sint"),rg32float:new m("rg32float",_.keyword,"rg32float"),rgba16uint:new m("rgba16uint",_.keyword,"rgba16uint"),rgba16sint:new m("rgba16sint",_.keyword,"rgba16sint"),rgba16float:new m("rgba16float",_.keyword,"rgba16float"),rgba32uint:new m("rgba32uint",_.keyword,"rgba32uint"),rgba32sint:new m("rgba32sint",_.keyword,"rgba32sint"),rgba32float:new m("rgba32float",_.keyword,"rgba32float"),static_assert:new m("static_assert",_.keyword,"static_assert")},f.tokens={decimal_float_literal:new m("decimal_float_literal",_.token,/((-?[0-9]*\.[0-9]+|-?[0-9]+\.[0-9]*)((e|E)(\+|-)?[0-9]+)?[fh]?)|(-?[0-9]+(e|E)(\+|-)?[0-9]+[fh]?)|(-?[0-9]+[fh])/),hex_float_literal:new m("hex_float_literal",_.token,/-?0x((([0-9a-fA-F]*\.[0-9a-fA-F]+|[0-9a-fA-F]+\.[0-9a-fA-F]*)((p|P)(\+|-)?[0-9]+[fh]?)?)|([0-9a-fA-F]+(p|P)(\+|-)?[0-9]+[fh]?))/),int_literal:new m("int_literal",_.token,/-?0x[0-9a-fA-F]+|0i?|-?[1-9][0-9]*i?/),uint_literal:new m("uint_literal",_.token,/0x[0-9a-fA-F]+u|0u|[1-9][0-9]*u/),name:new m("name",_.token,/([_\p{XID_Start}][\p{XID_Continue}]+)|([\p{XID_Start}])/u),ident:new m("ident",_.token,/[_a-zA-Z][0-9a-zA-Z_]*/),and:new m("and",_.token,"&"),and_and:new m("and_and",_.token,"&&"),arrow:new m("arrow ",_.token,"->"),attr:new m("attr",_.token,"@"),forward_slash:new m("forward_slash",_.token,"/"),bang:new m("bang",_.token,"!"),bracket_left:new m("bracket_left",_.token,"["),bracket_right:new m("bracket_right",_.token,"]"),brace_left:new m("brace_left",_.token,"{"),brace_right:new m("brace_right",_.token,"}"),colon:new m("colon",_.token,":"),comma:new m("comma",_.token,","),equal:new m("equal",_.token,"="),equal_equal:new m("equal_equal",_.token,"=="),not_equal:new m("not_equal",_.token,"!="),greater_than:new m("greater_than",_.token,">"),greater_than_equal:new m("greater_than_equal",_.token,">="),shift_right:new m("shift_right",_.token,">>"),less_than:new m("less_than",_.token,"<"),less_than_equal:new m("less_than_equal",_.token,"<="),shift_left:new m("shift_left",_.token,"<<"),modulo:new m("modulo",_.token,"%"),minus:new m("minus",_.token,"-"),minus_minus:new m("minus_minus",_.token,"--"),period:new m("period",_.token,"."),plus:new m("plus",_.token,"+"),plus_plus:new m("plus_plus",_.token,"++"),or:new m("or",_.token,"|"),or_or:new m("or_or",_.token,"||"),paren_left:new m("paren_left",_.token,"("),paren_right:new m("paren_right",_.token,")"),semicolon:new m("semicolon",_.token,";"),star:new m("star",_.token,"*"),tilde:new m("tilde",_.token,"~"),underscore:new m("underscore",_.token,"_"),xor:new m("xor",_.token,"^"),plus_equal:new m("plus_equal",_.token,"+="),minus_equal:new m("minus_equal",_.token,"-="),times_equal:new m("times_equal",_.token,"*="),division_equal:new m("division_equal",_.token,"/="),modulo_equal:new m("modulo_equal",_.token,"%="),and_equal:new m("and_equal",_.token,"&="),or_equal:new m("or_equal",_.token,"|="),xor_equal:new m("xor_equal",_.token,"^="),shift_right_equal:new m("shift_right_equal",_.token,">>="),shift_left_equal:new m("shift_left_equal",_.token,"<<=")},f.simpleTokens={"@":v.tokens.attr,"{":v.tokens.brace_left,"}":v.tokens.brace_right,":":v.tokens.colon,",":v.tokens.comma,"(":v.tokens.paren_left,")":v.tokens.paren_right,";":v.tokens.semicolon},f.literalTokens={"&":v.tokens.and,"&&":v.tokens.and_and,"->":v.tokens.arrow,"/":v.tokens.forward_slash,"!":v.tokens.bang,"[":v.tokens.bracket_left,"]":v.tokens.bracket_right,"=":v.tokens.equal,"==":v.tokens.equal_equal,"!=":v.tokens.not_equal,">":v.tokens.greater_than,">=":v.tokens.greater_than_equal,">>":v.tokens.shift_right,"<":v.tokens.less_than,"<=":v.tokens.less_than_equal,"<<":v.tokens.shift_left,"%":v.tokens.modulo,"-":v.tokens.minus,"--":v.tokens.minus_minus,".":v.tokens.period,"+":v.tokens.plus,"++":v.tokens.plus_plus,"|":v.tokens.or,"||":v.tokens.or_or,"*":v.tokens.star,"~":v.tokens.tilde,_:v.tokens.underscore,"^":v.tokens.xor,"+=":v.tokens.plus_equal,"-=":v.tokens.minus_equal,"*=":v.tokens.times_equal,"/=":v.tokens.division_equal,"%=":v.tokens.modulo_equal,"&=":v.tokens.and_equal,"|=":v.tokens.or_equal,"^=":v.tokens.xor_equal,">>=":v.tokens.shift_right_equal,"<<=":v.tokens.shift_left_equal},f.regexTokens={decimal_float_literal:v.tokens.decimal_float_literal,hex_float_literal:v.tokens.hex_float_literal,int_literal:v.tokens.int_literal,uint_literal:v.tokens.uint_literal,ident:v.tokens.ident},f.storage_class=[v.keywords.function,v.keywords.private,v.keywords.workgroup,v.keywords.uniform,v.keywords.storage],f.access_mode=[v.keywords.read,v.keywords.write,v.keywords.read_write],f.sampler_type=[v.keywords.sampler,v.keywords.sampler_comparison],f.sampled_texture_type=[v.keywords.texture_1d,v.keywords.texture_2d,v.keywords.texture_2d_array,v.keywords.texture_3d,v.keywords.texture_cube,v.keywords.texture_cube_array],f.multisampled_texture_type=[v.keywords.texture_multisampled_2d],f.storage_texture_type=[v.keywords.texture_storage_1d,v.keywords.texture_storage_2d,v.keywords.texture_storage_2d_array,v.keywords.texture_storage_3d],f.depth_texture_type=[v.keywords.texture_depth_2d,v.keywords.texture_depth_2d_array,v.keywords.texture_depth_cube,v.keywords.texture_depth_cube_array,v.keywords.texture_depth_multisampled_2d],f.texture_external_type=[v.keywords.texture_external],f.any_texture_type=[...v.sampled_texture_type,...v.multisampled_texture_type,...v.storage_texture_type,...v.depth_texture_type,...v.texture_external_type],f.texel_format=[v.keywords.r8unorm,v.keywords.r8snorm,v.keywords.r8uint,v.keywords.r8sint,v.keywords.r16uint,v.keywords.r16sint,v.keywords.r16float,v.keywords.rg8unorm,v.keywords.rg8snorm,v.keywords.rg8uint,v.keywords.rg8sint,v.keywords.r32uint,v.keywords.r32sint,v.keywords.r32float,v.keywords.rg16uint,v.keywords.rg16sint,v.keywords.rg16float,v.keywords.rgba8unorm,v.keywords.rgba8unorm_srgb,v.keywords.rgba8snorm,v.keywords.rgba8uint,v.keywords.rgba8sint,v.keywords.bgra8unorm,v.keywords.bgra8unorm_srgb,v.keywords.rgb10a2unorm,v.keywords.rg11b10float,v.keywords.rg32uint,v.keywords.rg32sint,v.keywords.rg32float,v.keywords.rgba16uint,v.keywords.rgba16sint,v.keywords.rgba16float,v.keywords.rgba32uint,v.keywords.rgba32sint,v.keywords.rgba32float],f.const_literal=[v.tokens.int_literal,v.tokens.uint_literal,v.tokens.decimal_float_literal,v.tokens.hex_float_literal,v.keywords.true,v.keywords.false],f.literal_or_ident=[v.tokens.ident,v.tokens.int_literal,v.tokens.uint_literal,v.tokens.decimal_float_literal,v.tokens.hex_float_literal,v.tokens.name],f.element_count_expression=[v.tokens.int_literal,v.tokens.uint_literal,v.tokens.ident],f.template_types=[v.keywords.vec2,v.keywords.vec3,v.keywords.vec4,v.keywords.mat2x2,v.keywords.mat2x3,v.keywords.mat2x4,v.keywords.mat3x2,v.keywords.mat3x3,v.keywords.mat3x4,v.keywords.mat4x2,v.keywords.mat4x3,v.keywords.mat4x4,v.keywords.atomic,v.keywords.bitcast,...v.any_texture_type],f.attribute_name=[v.tokens.ident,v.keywords.block,v.keywords.diagnostic],f.assignment_operators=[v.tokens.equal,v.tokens.plus_equal,v.tokens.minus_equal,v.tokens.times_equal,v.tokens.division_equal,v.tokens.modulo_equal,v.tokens.and_equal,v.tokens.or_equal,v.tokens.xor_equal,v.tokens.shift_right_equal,v.tokens.shift_left_equal],f.increment_operators=[v.tokens.plus_plus,v.tokens.minus_minus];var Lt=class{constructor(e,t,n,r,s){this.type=e,this.lexeme=t,this.line=n,this.start=r,this.end=s}toString(){return this.lexeme}isTemplateType(){return f.template_types.indexOf(this.type)!=-1}isArrayType(){return this.type==f.keywords.array}isArrayOrTemplateType(){return this.isArrayType()||this.isTemplateType()}},rn=class{constructor(e){this._tokens=[],this._start=0,this._current=0,this._line=1,this._source=e??""}scanTokens(){for(;!this._isAtEnd();)if(this._start=this._current,!this.scanToken())throw`Invalid syntax at line ${this._line}`;return this._tokens.push(new Lt(f.eof,"",this._line,this._current,this._current)),this._tokens}scanToken(){let e=this._advance();if(e==`
`)return this._line++,!0;if(this._isWhitespace(e))return!0;if(e=="/"){if(this._peekAhead()=="/"){for(;e!=`
`;){if(this._isAtEnd())return!0;e=this._advance()}return this._line++,!0}if(this._peekAhead()=="*"){this._advance();let i=1;for(;i>0;){if(this._isAtEnd())return!0;if(e=this._advance(),e==`
`)this._line++;else if(e=="*"){if(this._peekAhead()=="/"&&(this._advance(),i--,i==0))return!0}else e=="/"&&this._peekAhead()=="*"&&(this._advance(),i++)}return!0}}let t=f.simpleTokens[e];if(t)return this._addToken(t),!0;let n=f.none,r=this._isAlpha(e),s=e==="_";if(this._isAlphaNumeric(e)){let i=this._peekAhead();for(;this._isAlphaNumeric(i);)e+=this._advance(),i=this._peekAhead()}if(r){let i=f.keywords[e];if(i)return this._addToken(i),!0}if(r||s)return this._addToken(f.tokens.ident),!0;for(;;){let i=this._findType(e),o=this._peekAhead();if(e=="-"&&this._tokens.length>0){if(o=="=")return this._current++,e+=o,this._addToken(f.tokens.minus_equal),!0;if(o=="-")return this._current++,e+=o,this._addToken(f.tokens.minus_minus),!0;let l=this._tokens.length-1;if((f.literal_or_ident.indexOf(this._tokens[l].type)!=-1||this._tokens[l].type==f.tokens.paren_right)&&o!=">")return this._addToken(i),!0}if(e==">"&&(o==">"||o=="=")){let l=!1,c=this._tokens.length-1;for(let u=0;u<5&&c>=0&&f.assignment_operators.indexOf(this._tokens[c].type)===-1;++u,--c)if(this._tokens[c].type===f.tokens.less_than){c>0&&this._tokens[c-1].isArrayOrTemplateType()&&(l=!0);break}if(l)return this._addToken(i),!0}if(i===f.none){let l=e,c=0,u=2;for(let h=0;h<u;++h)if(l+=this._peekAhead(h),i=this._findType(l),i!==f.none){c=h;break}if(i===f.none)return n!==f.none&&(this._current--,this._addToken(n),!0);e=l,this._current+=c+1}if(n=i,this._isAtEnd())break;e+=this._advance()}return n!==f.none&&(this._addToken(n),!0)}_findType(e){for(let n in f.regexTokens){let r=f.regexTokens[n];if(this._match(e,r.rule))return r}return f.literalTokens[e]||f.none}_match(e,t){let n=t.exec(e);return n&&n.index==0&&n[0]==e}_isAtEnd(){return this._current>=this._source.length}_isAlpha(e){return!this._isNumeric(e)&&!this._isWhitespace(e)&&e!=="_"&&e!=="."&&e!=="("&&e!==")"&&e!=="["&&e!=="]"&&e!=="{"&&e!=="}"&&e!==","&&e!==";"&&e!==":"&&e!=="="&&e!=="!"&&e!=="<"&&e!==">"&&e!=="+"&&e!=="-"&&e!=="*"&&e!=="/"&&e!=="%"&&e!=="&"&&e!=="|"&&e!=="^"&&e!=="~"&&e!=="@"&&e!=="#"&&e!=="?"&&e!=="'"&&e!=="`"&&e!=='"'&&e!=="\\"&&e!==`
`&&e!=="\r"&&e!=="	"&&e!=="\0"}_isNumeric(e){return e>="0"&&e<="9"}_isAlphaNumeric(e){return this._isAlpha(e)||this._isNumeric(e)||e==="_"}_isWhitespace(e){return e==" "||e=="	"||e=="\r"}_advance(e=0){let t=this._source[this._current];return e=e||0,e++,this._current+=e,t}_peekAhead(e=0){return e=e||0,this._current+e>=this._source.length?"\0":this._source[this._current+e]}_addToken(e){let t=this._source.substring(this._start,this._current);this._tokens.push(new Lt(e,t,this._line,this._start,this._current))}};function w(a){return Array.isArray(a)||a?.buffer instanceof ArrayBuffer}var Et=new Float32Array(1),es=new Uint32Array(Et.buffer),ts=new Uint32Array(Et.buffer),Ct=new Int32Array(1),ns=new Float32Array(Ct.buffer),rs=new Uint32Array(Ct.buffer),Nt=new Uint32Array(1),ss=new Float32Array(Nt.buffer),os=new Int32Array(Nt.buffer);function jn(a,e,t){if(e===t)return a;if(e==="f32"){if(t==="i32"||t==="x32")return Et[0]=a,es[0];if(t==="u32")return Et[0]=a,ts[0]}else if(e==="i32"||e==="x32"){if(t==="f32")return Ct[0]=a,ns[0];if(t==="u32")return Ct[0]=a,rs[0]}else if(e==="u32"){if(t==="f32")return Nt[0]=a,ss[0];if(t==="i32"||t==="x32")return Nt[0]=a,os[0]}return console.error(`Unsupported cast from ${e} to ${t}`),a}var sn=class{constructor(e){this.resources=null,this.inUse=!1,this.info=null,this.node=e}},we=class{constructor(e,t){this.align=e,this.size=t}},Z=class{constructor(){this.uniforms=[],this.storage=[],this.textures=[],this.samplers=[],this.aliases=[],this.overrides=[],this.structs=[],this.entry=new Qt,this.functions=[],this._types=new Map,this._functions=new Map}_isStorageTexture(e){return e.name=="texture_storage_1d"||e.name=="texture_storage_2d"||e.name=="texture_storage_2d_array"||e.name=="texture_storage_3d"}updateAST(e){for(let t of e)t instanceof ge&&this._functions.set(t.name,new sn(t));for(let t of e)if(t instanceof Y){let n=this.getTypeInfo(t,null);n instanceof re&&this.structs.push(n)}for(let t of e)if(t instanceof $e)this.aliases.push(this._getAliasInfo(t));else if(t instanceof Ve){let n=t,r=this._getAttributeNum(n.attributes,"id",0),s=n.type!=null?this.getTypeInfo(n.type,n.attributes):null;this.overrides.push(new Xt(n.name,s,n.attributes,r))}else if(this._isUniformVar(t)){let n=t,r=this._getAttributeNum(n.attributes,"group",0),s=this._getAttributeNum(n.attributes,"binding",0),i=this.getTypeInfo(n.type,n.attributes),o=new xe(n.name,i,r,s,n.attributes,ce.Uniform,n.access);o.access||(o.access="read"),this.uniforms.push(o)}else if(this._isStorageVar(t)){let n=t,r=this._getAttributeNum(n.attributes,"group",0),s=this._getAttributeNum(n.attributes,"binding",0),i=this.getTypeInfo(n.type,n.attributes),o=this._isStorageTexture(i),l=new xe(n.name,i,r,s,n.attributes,o?ce.StorageTexture:ce.Storage,n.access);l.access||(l.access="read"),this.storage.push(l)}else if(this._isTextureVar(t)){let n=t,r=this._getAttributeNum(n.attributes,"group",0),s=this._getAttributeNum(n.attributes,"binding",0),i=this.getTypeInfo(n.type,n.attributes),o=this._isStorageTexture(i),l=new xe(n.name,i,r,s,n.attributes,o?ce.StorageTexture:ce.Texture,n.access);l.access||(l.access="read"),o?this.storage.push(l):this.textures.push(l)}else if(this._isSamplerVar(t)){let n=t,r=this._getAttributeNum(n.attributes,"group",0),s=this._getAttributeNum(n.attributes,"binding",0),i=this.getTypeInfo(n.type,n.attributes),o=new xe(n.name,i,r,s,n.attributes,ce.Sampler,n.access);this.samplers.push(o)}else if(t instanceof ge){let n=this._getAttribute(t,"vertex"),r=this._getAttribute(t,"fragment"),s=this._getAttribute(t,"compute"),i=n||r||s,o=new Zt(t.name,i?.name,t.attributes);o.attributes=t.attributes,o.startLine=t.startLine,o.endLine=t.endLine,this.functions.push(o),this._functions.get(t.name).info=o,i&&(this._functions.get(t.name).inUse=!0,o.inUse=!0,o.resources=this._findResources(t,!!i),o.inputs=this._getInputs(t.args),o.outputs=this._getOutputs(t.returnType),this.entry[i.name].push(o)),o.arguments=t.args.map(l=>new Kt(l.name,this.getTypeInfo(l.type,l.attributes),l.attributes)),o.returnType=t.returnType?this.getTypeInfo(t.returnType,t.attributes):null}for(let t of this._functions.values())t.info&&(t.info.inUse=t.inUse,this._addCalls(t.node,t.info.calls));for(let t of this._functions.values())t.node.search(n=>{var r,s,i;if(n instanceof Tt){if(n.value)if(w(n.value))for(let o of n.value)for(let l of this.overrides)o===l.name&&((r=t.info)===null||r===void 0||r.overrides.push(l));else for(let o of this.overrides)n.value===o.name&&((s=t.info)===null||s===void 0||s.overrides.push(o))}else if(n instanceof G)for(let o of this.overrides)n.name===o.name&&((i=t.info)===null||i===void 0||i.overrides.push(o))});for(let t of this.uniforms)this._markStructsInUse(t.type);for(let t of this.storage)this._markStructsInUse(t.type)}getStructInfo(e){for(let t of this.structs)if(t.name==e)return t;return null}getOverrideInfo(e){for(let t of this.overrides)if(t.name==e)return t;return null}_markStructsInUse(e){if(e)if(e.isStruct){if(e.inUse=!0,e.members)for(let t of e.members)this._markStructsInUse(t.type)}else if(e.isArray)this._markStructsInUse(e.format);else if(e.isTemplate)e.format&&this._markStructsInUse(e.format);else{let t=this._getAlias(e.name);t&&this._markStructsInUse(t)}}_addCalls(e,t){var n;for(let r of e.calls){let s=(n=this._functions.get(r.name))===null||n===void 0?void 0:n.info;s&&t.add(s)}}findResource(e,t,n){if(n){for(let r of this.entry.compute)if(r.name===n){for(let s of r.resources)if(s.group==e&&s.binding==t)return s}for(let r of this.entry.vertex)if(r.name===n){for(let s of r.resources)if(s.group==e&&s.binding==t)return s}for(let r of this.entry.fragment)if(r.name===n){for(let s of r.resources)if(s.group==e&&s.binding==t)return s}}for(let r of this.uniforms)if(r.group==e&&r.binding==t)return r;for(let r of this.storage)if(r.group==e&&r.binding==t)return r;for(let r of this.textures)if(r.group==e&&r.binding==t)return r;for(let r of this.samplers)if(r.group==e&&r.binding==t)return r;return null}_findResource(e){for(let t of this.uniforms)if(t.name==e)return t;for(let t of this.storage)if(t.name==e)return t;for(let t of this.textures)if(t.name==e)return t;for(let t of this.samplers)if(t.name==e)return t;return null}_markStructsFromAST(e){let t=this.getTypeInfo(e,null);this._markStructsInUse(t)}_findResources(e,t){let n=[],r=this,s=[];return e.search(i=>{if(i instanceof Se)s.push({});else if(i instanceof Ae)s.pop();else if(i instanceof ee){let o=i;t&&o.type!==null&&this._markStructsFromAST(o.type),s.length>0&&(s[s.length-1][o.name]=o)}else if(i instanceof K){let o=i;t&&o.type!==null&&this._markStructsFromAST(o.type)}else if(i instanceof me){let o=i;t&&o.type!==null&&this._markStructsFromAST(o.type),s.length>0&&(s[s.length-1][o.name]=o)}else if(i instanceof G){let o=i;if(s.length>0&&s[s.length-1][o.name])return;let l=r._findResource(o.name);l&&n.push(l)}else if(i instanceof He){let o=i,l=r._functions.get(o.name);l&&(t&&(l.inUse=!0),e.calls.add(l.node),l.resources===null&&(l.resources=r._findResources(l.node,t)),n.push(...l.resources))}else if(i instanceof Ue){let o=i,l=r._functions.get(o.name);l&&(t&&(l.inUse=!0),e.calls.add(l.node),l.resources===null&&(l.resources=r._findResources(l.node,t)),n.push(...l.resources))}}),[...new Map(n.map(i=>[i.name,i])).values()]}getBindGroups(){let e=[];function t(n,r){n>=e.length&&(e.length=n+1),e[n]===void 0&&(e[n]=[]),r>=e[n].length&&(e[n].length=r+1)}for(let n of this.uniforms)t(n.group,n.binding),e[n.group][n.binding]=n;for(let n of this.storage)t(n.group,n.binding),e[n.group][n.binding]=n;for(let n of this.textures)t(n.group,n.binding),e[n.group][n.binding]=n;for(let n of this.samplers)t(n.group,n.binding),e[n.group][n.binding]=n;return e}_getOutputs(e,t=void 0){if(t===void 0&&(t=[]),e instanceof Y)this._getStructOutputs(e,t);else{let n=this._getOutputInfo(e);n!==null&&t.push(n)}return t}_getStructOutputs(e,t){for(let n of e.members)if(n.type instanceof Y)this._getStructOutputs(n.type,t);else{let r=this._getAttribute(n,"location")||this._getAttribute(n,"builtin");if(r!==null){let s=this.getTypeInfo(n.type,n.type.attributes),i=this._parseInt(r.value),o=new it(n.name,s,r.name,i);t.push(o)}}}_getOutputInfo(e){let t=this._getAttribute(e,"location")||this._getAttribute(e,"builtin");if(t!==null){let n=this.getTypeInfo(e,e.attributes),r=this._parseInt(t.value);return new it("",n,t.name,r)}return null}_getInputs(e,t=void 0){t===void 0&&(t=[]);for(let n of e)if(n.type instanceof Y)this._getStructInputs(n.type,t);else{let r=this._getInputInfo(n);r!==null&&t.push(r)}return t}_getStructInputs(e,t){for(let n of e.members)if(n.type instanceof Y)this._getStructInputs(n.type,t);else{let r=this._getInputInfo(n);r!==null&&t.push(r)}}_getInputInfo(e){let t=this._getAttribute(e,"location")||this._getAttribute(e,"builtin");if(t!==null){let n=this._getAttribute(e,"interpolation"),r=this.getTypeInfo(e.type,e.attributes),s=this._parseInt(t.value),i=new jt(e.name,r,t.name,s);return n!==null&&(i.interpolation=this._parseString(n.value)),i}return null}_parseString(e){return e instanceof Array&&(e=e[0]),e}_parseInt(e){e instanceof Array&&(e=e[0]);let t=parseInt(e);return isNaN(t)?e:t}_getAlias(e){for(let t of this.aliases)if(t.name==e)return t.type;return null}_getAliasInfo(e){return new qt(e.name,this.getTypeInfo(e.type,null))}getTypeInfoByName(e){for(let t of this.structs)if(t.name==e)return t;for(let t of this.aliases)if(t.name==e)return t.type;return null}getTypeInfo(e,t=null){if(this._types.has(e))return this._types.get(e);if(e instanceof _e){let r=e,s=r.format?this.getTypeInfo(r.format,r.attributes):null,i=new se(r.name,t);return i.format=s,i.count=r.count,this._types.set(e,i),this._updateTypeInfo(i),i}if(e instanceof Y){let r=e,s=new re(r.name,t);s.startLine=r.startLine,s.endLine=r.endLine;for(let i of r.members){let o=this.getTypeInfo(i.type,i.attributes);s.members.push(new ot(i.name,o,i.attributes))}return this._types.set(e,s),this._updateTypeInfo(s),s}if(e instanceof de){let r=e,s=r.format instanceof x,i=r.format?s?this.getTypeInfo(r.format,null):new W(r.format,null):null,o=new ie(r.name,i,t,r.access);return this._types.set(e,o),this._updateTypeInfo(o),o}if(e instanceof g){let r=e,s=r.format?this.getTypeInfo(r.format,null):null,i=new ie(r.name,s,t,r.access);return this._types.set(e,i),this._updateTypeInfo(i),i}let n=new W(e.name,t);return this._types.set(e,n),this._updateTypeInfo(n),n}_updateTypeInfo(e){var t,n,r;let s=this._getTypeSize(e);if(e.size=(t=s?.size)!==null&&t!==void 0?t:0,e instanceof se&&e.format){let i=this._getTypeSize(e.format);e.stride=Math.max((n=i?.size)!==null&&n!==void 0?n:0,(r=i?.align)!==null&&r!==void 0?r:0),this._updateTypeInfo(e.format)}e instanceof re&&this._updateStructInfo(e)}_updateStructInfo(e){var t;let n=0,r=0,s=0,i=0;for(let o=0,l=e.members.length;o<l;++o){let c=e.members[o],u=this._getTypeSize(c);if(!u)continue;(t=this._getAlias(c.type.name))!==null&&t!==void 0||c.type;let h=u.align,b=u.size;n=this._roundUp(h,n+r),r=b,s=n,i=Math.max(i,h),c.offset=n,c.size=b,this._updateTypeInfo(c.type)}e.size=this._roundUp(i,s+r),e.align=i}_getTypeSize(e){var t,n;if(e==null)return null;let r=this._getAttributeNum(e.attributes,"size",0),s=this._getAttributeNum(e.attributes,"align",0);if(e instanceof ot&&(e=e.type),e instanceof W){let i=this._getAlias(e.name);i!==null&&(e=i)}{let i=Z._typeInfo[e.name];if(i!==void 0){let o=((t=e.format)===null||t===void 0?void 0:t.name)==="f16"?2:1;return new we(Math.max(s,i.align/o),Math.max(r,i.size/o))}}{let i=Z._typeInfo[e.name.substring(0,e.name.length-1)];if(i){let o=e.name[e.name.length-1]==="h"?2:1;return new we(Math.max(s,i.align/o),Math.max(r,i.size/o))}}if(e instanceof se){let i=e,o=8,l=8,c=this._getTypeSize(i.format);return c!==null&&(l=c.size,o=c.align),l=i.count*this._getAttributeNum((n=e?.attributes)!==null&&n!==void 0?n:null,"stride",this._roundUp(o,l)),r&&(l=r),new we(Math.max(s,o),Math.max(r,l))}if(e instanceof re){let i=0,o=0,l=0,c=0,u=0;for(let h of e.members){let b=this._getTypeSize(h.type);b!==null&&(i=Math.max(b.align,i),l=this._roundUp(b.align,l+c),c=b.size,u=l)}return o=this._roundUp(i,u+c),new we(Math.max(s,i),Math.max(r,o))}return null}_isUniformVar(e){return e instanceof ee&&e.storage=="uniform"}_isStorageVar(e){return e instanceof ee&&e.storage=="storage"}_isTextureVar(e){return e instanceof ee&&e.type!==null&&Z._textureTypes.indexOf(e.type.name)!=-1}_isSamplerVar(e){return e instanceof ee&&e.type!==null&&Z._samplerTypes.indexOf(e.type.name)!=-1}_getAttribute(e,t){let n=e;if(!n||!n.attributes)return null;let r=n.attributes;for(let s of r)if(s.name==t)return s;return null}_getAttributeNum(e,t,n){if(e===null)return n;for(let r of e)if(r.name==t){let s=r!==null&&r.value!==null?r.value:n;return s instanceof Array&&(s=s[0]),typeof s=="number"?s:typeof s=="string"?parseInt(s):n}return n}_roundUp(e,t){return Math.ceil(t/e)*e}};Z._typeInfo={f16:{align:2,size:2},i32:{align:4,size:4},u32:{align:4,size:4},f32:{align:4,size:4},atomic:{align:4,size:4},vec2:{align:8,size:8},vec3:{align:16,size:12},vec4:{align:16,size:16},mat2x2:{align:8,size:16},mat3x2:{align:8,size:24},mat4x2:{align:8,size:32},mat2x3:{align:16,size:32},mat3x3:{align:16,size:48},mat4x3:{align:16,size:64},mat2x4:{align:16,size:32},mat3x4:{align:16,size:48},mat4x4:{align:16,size:64}},Z._textureTypes=f.any_texture_type.map(a=>a.name),Z._samplerTypes=f.sampler_type.map(a=>a.name);var We=class{constructor(e,t,n){this.name=e,this.value=t,this.node=n}clone(){return new We(this.name,this.value,this.node)}},ze=class{constructor(e){this.name=e.name,this.node=e}clone(){return new ze(this.node)}},qe=class{constructor(e){this.parent=null,this.variables=new Map,this.functions=new Map,this.currentFunctionName="",e&&(this.parent=e,this.currentFunctionName=e.currentFunctionName)}getVariable(e){var t;return this.variables.has(e)?(t=this.variables.get(e))!==null&&t!==void 0?t:null:this.parent?this.parent.getVariable(e):null}getFunction(e){var t;return this.functions.has(e)?(t=this.functions.get(e))!==null&&t!==void 0?t:null:this.parent?this.parent.getFunction(e):null}createVariable(e,t,n){this.variables.set(e,new We(e,t,n??null))}setVariable(e,t,n){let r=this.getVariable(e);r!==null?r.value=t:this.createVariable(e,t,n)}getVariableValue(e){var t;let n=this.getVariable(e);return(t=n?.value)!==null&&t!==void 0?t:null}clone(){return new qe(this)}},on=class{evalExpression(e,t){return null}getTypeInfo(e){return null}getVariableName(e,t){return""}},an=class{constructor(e){this.exec=e}getTypeInfo(e){return this.exec.getTypeInfo(e)}All(e,t){let n=this.exec.evalExpression(e.args[0],t),r=!0;if(n instanceof p)return n.data.forEach(s=>{s||(r=!1)}),new d(r?1:0,this.getTypeInfo("bool"));throw new Error(`All() expects a vector argument. Line ${e.line}`)}Any(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p){let r=n.data.some(s=>s);return new d(r?1:0,this.getTypeInfo("bool"))}throw new Error(`Any() expects a vector argument. Line ${e.line}`)}Select(e,t){let n=this.exec.evalExpression(e.args[2],t);if(!(n instanceof d))throw new Error(`Select() expects a bool condition. Line ${e.line}`);return n.value?this.exec.evalExpression(e.args[1],t):this.exec.evalExpression(e.args[0],t)}ArrayLength(e,t){let n=e.args[0];n instanceof M&&(n=n.right);let r=this.exec.evalExpression(n,t);if(r instanceof D&&r.typeInfo.size===0){let s=r.typeInfo,i=r.buffer.byteLength/s.stride;return new d(i,this.getTypeInfo("u32"))}return new d(r.typeInfo.size,this.getTypeInfo("u32"))}Abs(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.abs(s)),n.typeInfo);let r=n;return new d(Math.abs(r.value),r.typeInfo)}Acos(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.acos(s)),n.typeInfo);let r=n;return new d(Math.acos(r.value),n.typeInfo)}Acosh(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.acosh(s)),n.typeInfo);let r=n;return new d(Math.acosh(r.value),n.typeInfo)}Asin(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.asin(s)),n.typeInfo);let r=n;return new d(Math.asin(r.value),n.typeInfo)}Asinh(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.asinh(s)),n.typeInfo);let r=n;return new d(Math.asinh(r.value),n.typeInfo)}Atan(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.atan(s)),n.typeInfo);let r=n;return new d(Math.atan(r.value),n.typeInfo)}Atanh(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.atanh(s)),n.typeInfo);let r=n;return new d(Math.atanh(r.value),n.typeInfo)}Atan2(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t);if(n instanceof p&&r instanceof p)return new p(n.data.map((o,l)=>Math.atan2(o,r.data[l])),n.typeInfo);let s=n,i=r;return new d(Math.atan2(s.value,i.value),n.typeInfo)}Ceil(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.ceil(s)),n.typeInfo);let r=n;return new d(Math.ceil(r.value),n.typeInfo)}_clamp(e,t,n){return Math.min(Math.max(e,t),n)}Clamp(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t),s=this.exec.evalExpression(e.args[2],t);if(n instanceof p&&r instanceof p&&s instanceof p)return new p(n.data.map((c,u)=>this._clamp(c,r.data[u],s.data[u])),n.typeInfo);let i=n,o=r,l=s;return new d(this._clamp(i.value,o.value,l.value),n.typeInfo)}Cos(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.cos(s)),n.typeInfo);let r=n;return new d(Math.cos(r.value),n.typeInfo)}Cosh(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.cosh(s)),n.typeInfo);let r=n;return new d(Math.cos(r.value),n.typeInfo)}CountLeadingZeros(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.clz32(s)),n.typeInfo);let r=n;return new d(Math.clz32(r.value),n.typeInfo)}_countOneBits(e){let t=0;for(;e!==0;)1&e&&t++,e>>=1;return t}CountOneBits(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>this._countOneBits(s)),n.typeInfo);let r=n;return new d(this._countOneBits(r.value),n.typeInfo)}_countTrailingZeros(e){if(e===0)return 32;let t=0;for(;!(1&e);)e>>=1,t++;return t}CountTrailingZeros(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>this._countTrailingZeros(s)),n.typeInfo);let r=n;return new d(this._countTrailingZeros(r.value),n.typeInfo)}Cross(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t);if(n instanceof p&&r instanceof p){if(n.data.length!==3||r.data.length!==3)return console.error(`Cross() expects 3D vectors. Line ${e.line}`),null;let s=n.data,i=r.data;return new p([s[1]*i[2]-i[1]*s[2],s[2]*i[0]-i[2]*s[0],s[0]*i[1]-i[0]*s[1]],n.typeInfo)}return console.error(`Cross() expects vector arguments. Line ${e.line}`),null}Degrees(e,t){let n=this.exec.evalExpression(e.args[0],t),r=180/Math.PI;return n instanceof p?new p(n.data.map(s=>s*r),n.typeInfo):new d(n.value*r,this.getTypeInfo("f32"))}Determinant(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof I){let r=n.data,s=n.typeInfo.getTypeName(),i=s.endsWith("h")?this.getTypeInfo("f16"):this.getTypeInfo("f32");if(s==="mat2x2"||s==="mat2x2f"||s==="mat2x2h")return new d(r[0]*r[3]-r[1]*r[2],i);if(s==="mat2x3"||s==="mat2x3f"||s==="mat2x3h")return new d(r[0]*(r[4]*r[8]-r[5]*r[7])-r[1]*(r[3]*r[8]-r[5]*r[6])+r[2]*(r[3]*r[7]-r[4]*r[6]),i);if(s==="mat2x4"||s==="mat2x4f"||s==="mat2x4h")console.error(`TODO: Determinant for ${s}`);else if(s==="mat3x2"||s==="mat3x2f"||s==="mat3x2h")console.error(`TODO: Determinant for ${s}`);else{if(s==="mat3x3"||s==="mat3x3f"||s==="mat3x3h")return new d(r[0]*(r[4]*r[8]-r[5]*r[7])-r[1]*(r[3]*r[8]-r[5]*r[6])+r[2]*(r[3]*r[7]-r[4]*r[6]),i);s==="mat3x4"||s==="mat3x4f"||s==="mat3x4h"||s==="mat4x2"||s==="mat4x2f"||s==="mat4x2h"||s==="mat4x3"||s==="mat4x3f"||s==="mat4x3h"?console.error(`TODO: Determinant for ${s}`):s!=="mat4x4"&&s!=="mat4x4f"&&s!=="mat4x4h"||console.error(`TODO: Determinant for ${s}`)}}return console.error(`Determinant expects a matrix argument. Line ${e.line}`),null}Distance(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t);if(n instanceof p&&r instanceof p){let o=0;for(let l=0;l<n.data.length;++l)o+=(n.data[l]-r.data[l])*(n.data[l]-r.data[l]);return new d(Math.sqrt(o),this.getTypeInfo("f32"))}let s=n,i=r;return new d(Math.abs(s.value-i.value),n.typeInfo)}_dot(e,t){let n=0;for(let r=0;r<e.length;++r)n+=t[r]*e[r];return n}Dot(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t);return n instanceof p&&r instanceof p?new d(this._dot(n.data,r.data),this.getTypeInfo("f32")):(console.error(`Dot() expects vector arguments. Line ${e.line}`),null)}Dot4U8Packed(e,t){return console.error(`TODO: dot4U8Packed. Line ${e.line}`),null}Dot4I8Packed(e,t){return console.error(`TODO: dot4I8Packed. Line ${e.line}`),null}Exp(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.exp(s)),n.typeInfo);let r=n;return new d(Math.exp(r.value),n.typeInfo)}Exp2(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.pow(2,s)),n.typeInfo);let r=n;return new d(Math.pow(2,r.value),n.typeInfo)}ExtractBits(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t),s=this.exec.evalExpression(e.args[2],t);if(r.typeInfo.name!=="u32"&&r.typeInfo.name!=="x32")return console.error(`ExtractBits() expects an i32 offset argument. Line ${e.line}`),null;if(s.typeInfo.name!=="u32"&&s.typeInfo.name!=="x32")return console.error(`ExtractBits() expects an i32 count argument. Line ${e.line}`),null;let i=r.value,o=s.value;if(n instanceof p)return new p(n.data.map(c=>c>>i&(1<<o)-1),n.typeInfo);if(n.typeInfo.name!=="i32"&&n.typeInfo.name!=="x32")return console.error(`ExtractBits() expects an i32 argument. Line ${e.line}`),null;let l=n.value;return new d(l>>i&(1<<o)-1,this.getTypeInfo("i32"))}FaceForward(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t),s=this.exec.evalExpression(e.args[2],t);if(n instanceof p&&r instanceof p&&s instanceof p){let i=this._dot(r.data,s.data);return new p(i<0?Array.from(n.data):n.data.map(o=>-o),n.typeInfo)}return console.error(`FaceForward() expects vector arguments. Line ${e.line}`),null}_firstLeadingBit(e){return e===0?-1:31-Math.clz32(e)}FirstLeadingBit(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>this._firstLeadingBit(s)),n.typeInfo);let r=n;return new d(this._firstLeadingBit(r.value),n.typeInfo)}_firstTrailingBit(e){return e===0?-1:Math.log2(e&-e)}FirstTrailingBit(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>this._firstTrailingBit(s)),n.typeInfo);let r=n;return new d(this._firstTrailingBit(r.value),n.typeInfo)}Floor(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.floor(s)),n.typeInfo);let r=n;return new d(Math.floor(r.value),n.typeInfo)}Fma(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t),s=this.exec.evalExpression(e.args[2],t);if(n instanceof p&&r instanceof p&&s instanceof p)return n.data.length!==r.data.length||n.data.length!==s.data.length?(console.error(`Fma() expects vectors of the same length. Line ${e.line}`),null):new p(n.data.map((c,u)=>c*r.data[u]+s.data[u]),n.typeInfo);let i=n,o=r,l=s;return new d(i.value*o.value+l.value,i.typeInfo)}Fract(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>s-Math.floor(s)),n.typeInfo);let r=n;return new d(r.value-Math.floor(r.value),n.typeInfo)}Frexp(e,t){return console.error(`TODO: frexp. Line ${e.line}`),null}InsertBits(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t),s=this.exec.evalExpression(e.args[2],t),i=this.exec.evalExpression(e.args[3],t);if(s.typeInfo.name!=="u32"&&s.typeInfo.name!=="x32")return console.error(`InsertBits() expects an i32 offset argument. Line ${e.line}`),null;let o=s.value,l=(1<<i.value)-1<<o,c=~l;if(n instanceof p&&r instanceof p)return new p(n.data.map((b,y)=>b&c|r.data[y]<<o&l),n.typeInfo);let u=n.value,h=r.value;return new d(u&c|h<<o&l,n.typeInfo)}InverseSqrt(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>1/Math.sqrt(s)),n.typeInfo);let r=n;return new d(1/Math.sqrt(r.value),n.typeInfo)}Ldexp(e,t){return console.error(`TODO: ldexp. Line ${e.line}`),null}Length(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p){let s=0;return n.data.forEach(i=>{s+=i*i}),new d(Math.sqrt(s),this.getTypeInfo("f32"))}let r=n;return new d(Math.abs(r.value),n.typeInfo)}Log(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.log(s)),n.typeInfo);let r=n;return new d(Math.log(r.value),n.typeInfo)}Log2(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.log2(s)),n.typeInfo);let r=n;return new d(Math.log2(r.value),n.typeInfo)}Max(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t);if(n instanceof p&&r instanceof p)return new p(n.data.map((o,l)=>Math.max(o,r.data[l])),n.typeInfo);let s=n,i=r;return new d(Math.max(s.value,i.value),n.typeInfo)}Min(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t);if(n instanceof p&&r instanceof p)return new p(n.data.map((o,l)=>Math.min(o,r.data[l])),n.typeInfo);let s=n,i=r;return new d(Math.min(s.value,i.value),n.typeInfo)}Mix(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t),s=this.exec.evalExpression(e.args[2],t);if(n instanceof p&&r instanceof p&&s instanceof p)return new p(n.data.map((l,c)=>n.data[c]*(1-s.data[c])+r.data[c]*s.data[c]),n.typeInfo);let i=r,o=s;return new d(n.value*(1-o.value)+i.value*o.value,n.typeInfo)}Modf(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t);if(n instanceof p&&r instanceof p)return new p(n.data.map((i,o)=>i%r.data[o]),n.typeInfo);let s=r;return new d(n.value%s.value,n.typeInfo)}Normalize(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p){let r=this.Length(e,t).value;return new p(n.data.map(s=>s/r),n.typeInfo)}return console.error(`Normalize() expects a vector argument. Line ${e.line}`),null}Pow(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t);if(n instanceof p&&r instanceof p)return new p(n.data.map((o,l)=>Math.pow(o,r.data[l])),n.typeInfo);let s=n,i=r;return new d(Math.pow(s.value,i.value),n.typeInfo)}QuantizeToF16(e,t){let n=this.exec.evalExpression(e.args[0],t);return n instanceof p?new p(n.data.map(r=>r),n.typeInfo):new d(n.value,n.typeInfo)}Radians(e,t){let n=this.exec.evalExpression(e.args[0],t);return n instanceof p?new p(n.data.map(r=>r*Math.PI/180),n.typeInfo):new d(n.value*Math.PI/180,this.getTypeInfo("f32"))}Reflect(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t);if(n instanceof p&&r instanceof p){let s=this._dot(n.data,r.data);return new p(n.data.map((i,o)=>i-2*s*r.data[o]),n.typeInfo)}return console.error(`Reflect() expects vector arguments. Line ${e.line}`),null}Refract(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t),s=this.exec.evalExpression(e.args[2],t);if(n instanceof p&&r instanceof p&&s instanceof d){let i=this._dot(r.data,n.data);return new p(n.data.map((o,l)=>{let c=1-s.value*s.value*(1-i*i);if(c<0)return 0;let u=Math.sqrt(c);return s.value*o-(s.value*i+u)*r.data[l]}),n.typeInfo)}return console.error(`Refract() expects vector arguments and a scalar argument. Line ${e.line}`),null}ReverseBits(e,t){return console.error(`TODO: reverseBits. Line ${e.line}`),null}Round(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.round(s)),n.typeInfo);let r=n;return new d(Math.round(r.value),n.typeInfo)}Saturate(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.min(Math.max(s,0),1)),n.typeInfo);let r=n;return new d(Math.min(Math.max(r.value,0),1),n.typeInfo)}Sign(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.sign(s)),n.typeInfo);let r=n;return new d(Math.sign(r.value),n.typeInfo)}Sin(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.sin(s)),n.typeInfo);let r=n;return new d(Math.sin(r.value),n.typeInfo)}Sinh(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.sinh(s)),n.typeInfo);let r=n;return new d(Math.sinh(r.value),n.typeInfo)}_smoothstep(e,t,n){let r=Math.min(Math.max((n-e)/(t-e),0),1);return r*r*(3-2*r)}SmoothStep(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t),s=this.exec.evalExpression(e.args[2],t);if(s instanceof p&&n instanceof p&&r instanceof p)return new p(s.data.map((c,u)=>this._smoothstep(n.data[u],r.data[u],c)),s.typeInfo);let i=n,o=r,l=s;return new d(this._smoothstep(i.value,o.value,l.value),s.typeInfo)}Sqrt(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.sqrt(s)),n.typeInfo);let r=n;return new d(Math.sqrt(r.value),n.typeInfo)}Step(e,t){let n=this.exec.evalExpression(e.args[0],t),r=this.exec.evalExpression(e.args[1],t);if(r instanceof p&&n instanceof p)return new p(r.data.map((i,o)=>i<n.data[o]?0:1),r.typeInfo);let s=n;return new d(r.value<s.value?0:1,s.typeInfo)}Tan(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.tan(s)),n.typeInfo);let r=n;return new d(Math.tan(r.value),n.typeInfo)}Tanh(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.tanh(s)),n.typeInfo);let r=n;return new d(Math.tanh(r.value),n.typeInfo)}_getTransposeType(e){let t=e.getTypeName();return t==="mat2x2f"||t==="mat2x2h"?e:t==="mat2x3f"?this.getTypeInfo("mat3x2f"):t==="mat2x3h"?this.getTypeInfo("mat3x2h"):t==="mat2x4f"?this.getTypeInfo("mat4x2f"):t==="mat2x4h"?this.getTypeInfo("mat4x2h"):t==="mat3x2f"?this.getTypeInfo("mat2x3f"):t==="mat3x2h"?this.getTypeInfo("mat2x3h"):t==="mat3x3f"||t==="mat3x3h"?e:t==="mat3x4f"?this.getTypeInfo("mat4x3f"):t==="mat3x4h"?this.getTypeInfo("mat4x3h"):t==="mat4x2f"?this.getTypeInfo("mat2x4f"):t==="mat4x2h"?this.getTypeInfo("mat2x4h"):t==="mat4x3f"?this.getTypeInfo("mat3x4f"):t==="mat4x3h"?this.getTypeInfo("mat3x4h"):(t==="mat4x4f"||t==="mat4x4h"||console.error(`Invalid matrix type ${t}`),e)}Transpose(e,t){let n=this.exec.evalExpression(e.args[0],t);if(!(n instanceof I))return console.error(`Transpose() expects a matrix argument. Line ${e.line}`),null;let r=this._getTransposeType(n.typeInfo);if(n.typeInfo.name==="mat2x2"||n.typeInfo.name==="mat2x2f"||n.typeInfo.name==="mat2x2h"){let s=n.data;return new I([s[0],s[2],s[1],s[3]],r)}if(n.typeInfo.name==="mat2x3"||n.typeInfo.name==="mat2x3f"||n.typeInfo.name==="mat2x3h"){let s=n.data;return new I([s[0],s[3],s[6],s[1],s[4],s[7]],r)}if(n.typeInfo.name==="mat2x4"||n.typeInfo.name==="mat2x4f"||n.typeInfo.name==="mat2x4h"){let s=n.data;return new I([s[0],s[4],s[8],s[12],s[1],s[5],s[9],s[13]],r)}if(n.typeInfo.name==="mat3x2"||n.typeInfo.name==="mat3x2f"||n.typeInfo.name==="mat3x2h"){let s=n.data;return new I([s[0],s[3],s[1],s[4],s[2],s[5]],r)}if(n.typeInfo.name==="mat3x3"||n.typeInfo.name==="mat3x3f"||n.typeInfo.name==="mat3x3h"){let s=n.data;return new I([s[0],s[3],s[6],s[1],s[4],s[7],s[2],s[5],s[8]],r)}if(n.typeInfo.name==="mat3x4"||n.typeInfo.name==="mat3x4f"||n.typeInfo.name==="mat3x4h"){let s=n.data;return new I([s[0],s[4],s[8],s[12],s[1],s[5],s[9],s[13],s[2],s[6],s[10],s[14]],r)}if(n.typeInfo.name==="mat4x2"||n.typeInfo.name==="mat4x2f"||n.typeInfo.name==="mat4x2h"){let s=n.data;return new I([s[0],s[4],s[1],s[5],s[2],s[6]],r)}if(n.typeInfo.name==="mat4x3"||n.typeInfo.name==="mat4x3f"||n.typeInfo.name==="mat4x3h"){let s=n.data;return new I([s[0],s[4],s[8],s[1],s[5],s[9],s[2],s[6],s[10]],r)}if(n.typeInfo.name==="mat4x4"||n.typeInfo.name==="mat4x4f"||n.typeInfo.name==="mat4x4h"){let s=n.data;return new I([s[0],s[4],s[8],s[12],s[1],s[5],s[9],s[13],s[2],s[6],s[10],s[14],s[3],s[7],s[11],s[15]],r)}return console.error(`Invalid matrix type ${n.typeInfo.name}`),null}Trunc(e,t){let n=this.exec.evalExpression(e.args[0],t);if(n instanceof p)return new p(n.data.map(s=>Math.trunc(s)),n.typeInfo);let r=n;return new d(Math.trunc(r.value),n.typeInfo)}Dpdx(e,t){return console.error(`TODO: dpdx. Line ${e.line}`),null}DpdxCoarse(e,t){return console.error(`TODO: dpdxCoarse. Line ${e.line}`),null}DpdxFine(e,t){return console.error("TODO: dpdxFine"),null}Dpdy(e,t){return console.error("TODO: dpdy"),null}DpdyCoarse(e,t){return console.error("TODO: dpdyCoarse"),null}DpdyFine(e,t){return console.error("TODO: dpdyFine"),null}Fwidth(e,t){return console.error("TODO: fwidth"),null}FwidthCoarse(e,t){return console.error("TODO: fwidthCoarse"),null}FwidthFine(e,t){return console.error("TODO: fwidthFine"),null}TextureDimensions(e,t){let n=e.args[0],r=e.args.length>1?this.exec.evalExpression(e.args[1],t).value:0;if(n instanceof G){let s=n.name,i=t.getVariableValue(s);if(i instanceof J){if(r<0||r>=i.mipLevelCount)return console.error(`Invalid mip level for textureDimensions. Line ${e.line}`),null;let o=i.getMipLevelSize(r),l=i.dimension;return l==="1d"?new d(o[0],this.getTypeInfo("u32")):l==="3d"?new p(o,this.getTypeInfo("vec3u")):l==="2d"?new p(o.slice(0,2),this.getTypeInfo("vec2u")):(console.error(`Invalid texture dimension ${l} not found. Line ${e.line}`),null)}return console.error(`Texture ${s} not found. Line ${e.line}`),null}return console.error(`Invalid texture argument for textureDimensions. Line ${e.line}`),null}TextureGather(e,t){return console.error("TODO: textureGather"),null}TextureGatherCompare(e,t){return console.error("TODO: textureGatherCompare"),null}TextureLoad(e,t){let n=e.args[0],r=this.exec.evalExpression(e.args[1],t),s=e.args.length>2?this.exec.evalExpression(e.args[2],t).value:0;if(!(r instanceof p)||r.data.length!==2)return console.error(`Invalid UV argument for textureLoad. Line ${e.line}`),null;if(n instanceof G){let i=n.name,o=t.getVariableValue(i);if(o instanceof J){let l=Math.floor(r.data[0]),c=Math.floor(r.data[1]);if(l<0||l>=o.width||c<0||c>=o.height)return console.error(`Texture ${i} out of bounds. Line ${e.line}`),null;let u=o.getPixel(l,c,0,s);return u===null?(console.error(`Invalid texture format for textureLoad. Line ${e.line}`),null):new p(u,this.getTypeInfo("vec4f"))}return console.error(`Texture ${i} not found. Line ${e.line}`),null}return console.error(`Invalid texture argument for textureLoad. Line ${e.line}`),null}TextureNumLayers(e,t){let n=e.args[0];if(n instanceof G){let r=n.name,s=t.getVariableValue(r);return s instanceof J?new d(s.depthOrArrayLayers,this.getTypeInfo("u32")):(console.error(`Texture ${r} not found. Line ${e.line}`),null)}return console.error(`Invalid texture argument for textureNumLayers. Line ${e.line}`),null}TextureNumLevels(e,t){let n=e.args[0];if(n instanceof G){let r=n.name,s=t.getVariableValue(r);return s instanceof J?new d(s.mipLevelCount,this.getTypeInfo("u32")):(console.error(`Texture ${r} not found. Line ${e.line}`),null)}return console.error(`Invalid texture argument for textureNumLevels. Line ${e.line}`),null}TextureNumSamples(e,t){let n=e.args[0];if(n instanceof G){let r=n.name,s=t.getVariableValue(r);return s instanceof J?new d(s.sampleCount,this.getTypeInfo("u32")):(console.error(`Texture ${r} not found. Line ${e.line}`),null)}return console.error(`Invalid texture argument for textureNumSamples. Line ${e.line}`),null}TextureSample(e,t){return console.error("TODO: textureSample"),null}TextureSampleBias(e,t){return console.error("TODO: textureSampleBias"),null}TextureSampleCompare(e,t){return console.error("TODO: textureSampleCompare"),null}TextureSampleCompareLevel(e,t){return console.error("TODO: textureSampleCompareLevel"),null}TextureSampleGrad(e,t){return console.error("TODO: textureSampleGrad"),null}TextureSampleLevel(e,t){return console.error("TODO: textureSampleLevel"),null}TextureSampleBaseClampToEdge(e,t){return console.error("TODO: textureSampleBaseClampToEdge"),null}TextureStore(e,t){let n=e.args[0],r=this.exec.evalExpression(e.args[1],t),s=e.args.length===4?this.exec.evalExpression(e.args[2],t).value:0,i=e.args.length===4?this.exec.evalExpression(e.args[3],t).data:this.exec.evalExpression(e.args[2],t).data;if(i.length!==4)return console.error(`Invalid value argument for textureStore. Line ${e.line}`),null;if(!(r instanceof p)||r.data.length!==2)return console.error(`Invalid UV argument for textureStore. Line ${e.line}`),null;if(n instanceof G){let o=n.name,l=t.getVariableValue(o);if(l instanceof J){let c=l.getMipLevelSize(0),u=Math.floor(r.data[0]),h=Math.floor(r.data[1]);return u<0||u>=c[0]||h<0||h>=c[1]?(console.error(`Texture ${o} out of bounds. Line ${e.line}`),null):(l.setPixel(u,h,0,s,Array.from(i)),null)}return console.error(`Texture ${o} not found. Line ${e.line}`),null}return console.error(`Invalid texture argument for textureStore. Line ${e.line}`),null}AtomicLoad(e,t){let n=e.args[0];n instanceof M&&(n=n.right);let r=this.exec.getVariableName(n,t);return t.getVariable(r).value.getSubData(this.exec,n.postfix,t)}AtomicStore(e,t){let n=e.args[0];n instanceof M&&(n=n.right);let r=this.exec.getVariableName(n,t),s=t.getVariable(r),i=e.args[1],o=this.exec.evalExpression(i,t),l=s.value.getSubData(this.exec,n.postfix,t);return l instanceof d&&o instanceof d&&(l.value=o.value),s.value instanceof D&&s.value.setDataValue(this.exec,l,n.postfix,t),null}AtomicAdd(e,t){let n=e.args[0];n instanceof M&&(n=n.right);let r=this.exec.getVariableName(n,t),s=t.getVariable(r),i=e.args[1],o=this.exec.evalExpression(i,t),l=s.value.getSubData(this.exec,n.postfix,t),c=new d(l.value,l.typeInfo);return l instanceof d&&o instanceof d&&(l.value+=o.value),s.value instanceof D&&s.value.setDataValue(this.exec,l,n.postfix,t),c}AtomicSub(e,t){let n=e.args[0];n instanceof M&&(n=n.right);let r=this.exec.getVariableName(n,t),s=t.getVariable(r),i=e.args[1],o=this.exec.evalExpression(i,t),l=s.value.getSubData(this.exec,n.postfix,t),c=new d(l.value,l.typeInfo);return l instanceof d&&o instanceof d&&(l.value-=o.value),s.value instanceof D&&s.value.setDataValue(this.exec,l,n.postfix,t),c}AtomicMax(e,t){let n=e.args[0];n instanceof M&&(n=n.right);let r=this.exec.getVariableName(n,t),s=t.getVariable(r),i=e.args[1],o=this.exec.evalExpression(i,t),l=s.value.getSubData(this.exec,n.postfix,t),c=new d(l.value,l.typeInfo);return l instanceof d&&o instanceof d&&(l.value=Math.max(l.value,o.value)),s.value instanceof D&&s.value.setDataValue(this.exec,l,n.postfix,t),c}AtomicMin(e,t){let n=e.args[0];n instanceof M&&(n=n.right);let r=this.exec.getVariableName(n,t),s=t.getVariable(r),i=e.args[1],o=this.exec.evalExpression(i,t),l=s.value.getSubData(this.exec,n.postfix,t),c=new d(l.value,l.typeInfo);return l instanceof d&&o instanceof d&&(l.value=Math.min(l.value,o.value)),s.value instanceof D&&s.value.setDataValue(this.exec,l,n.postfix,t),c}AtomicAnd(e,t){let n=e.args[0];n instanceof M&&(n=n.right);let r=this.exec.getVariableName(n,t),s=t.getVariable(r),i=e.args[1],o=this.exec.evalExpression(i,t),l=s.value.getSubData(this.exec,n.postfix,t),c=new d(l.value,l.typeInfo);return l instanceof d&&o instanceof d&&(l.value=l.value&o.value),s.value instanceof D&&s.value.setDataValue(this.exec,l,n.postfix,t),c}AtomicOr(e,t){let n=e.args[0];n instanceof M&&(n=n.right);let r=this.exec.getVariableName(n,t),s=t.getVariable(r),i=e.args[1],o=this.exec.evalExpression(i,t),l=s.value.getSubData(this.exec,n.postfix,t),c=new d(l.value,l.typeInfo);return l instanceof d&&o instanceof d&&(l.value=l.value|o.value),s.value instanceof D&&s.value.setDataValue(this.exec,l,n.postfix,t),c}AtomicXor(e,t){let n=e.args[0];n instanceof M&&(n=n.right);let r=this.exec.getVariableName(n,t),s=t.getVariable(r),i=e.args[1],o=this.exec.evalExpression(i,t),l=s.value.getSubData(this.exec,n.postfix,t),c=new d(l.value,l.typeInfo);return l instanceof d&&o instanceof d&&(l.value=l.value^o.value),s.value instanceof D&&s.value.setDataValue(this.exec,l,n.postfix,t),c}AtomicExchange(e,t){let n=e.args[0];n instanceof M&&(n=n.right);let r=this.exec.getVariableName(n,t),s=t.getVariable(r),i=e.args[1],o=this.exec.evalExpression(i,t),l=s.value.getSubData(this.exec,n.postfix,t),c=new d(l.value,l.typeInfo);return l instanceof d&&o instanceof d&&(l.value=o.value),s.value instanceof D&&s.value.setDataValue(this.exec,l,n.postfix,t),c}AtomicCompareExchangeWeak(e,t){return console.error("TODO: atomicCompareExchangeWeak"),null}Pack4x8snorm(e,t){return console.error("TODO: pack4x8snorm"),null}Pack4x8unorm(e,t){return console.error("TODO: pack4x8unorm"),null}Pack4xI8(e,t){return console.error("TODO: pack4xI8"),null}Pack4xU8(e,t){return console.error("TODO: pack4xU8"),null}Pack4x8Clamp(e,t){return console.error("TODO: pack4x8Clamp"),null}Pack4xU8Clamp(e,t){return console.error("TODO: pack4xU8Clamp"),null}Pack2x16snorm(e,t){return console.error("TODO: pack2x16snorm"),null}Pack2x16unorm(e,t){return console.error("TODO: pack2x16unorm"),null}Pack2x16float(e,t){return console.error("TODO: pack2x16float"),null}Unpack4x8snorm(e,t){return console.error("TODO: unpack4x8snorm"),null}Unpack4x8unorm(e,t){return console.error("TODO: unpack4x8unorm"),null}Unpack4xI8(e,t){return console.error("TODO: unpack4xI8"),null}Unpack4xU8(e,t){return console.error("TODO: unpack4xU8"),null}Unpack2x16snorm(e,t){return console.error("TODO: unpack2x16snorm"),null}Unpack2x16unorm(e,t){return console.error("TODO: unpack2x16unorm"),null}Unpack2x16float(e,t){return console.error("TODO: unpack2x16float"),null}StorageBarrier(e,t){return null}TextureBarrier(e,t){return null}WorkgroupBarrier(e,t){return null}WorkgroupUniformLoad(e,t){return null}SubgroupAdd(e,t){return console.error("TODO: subgroupAdd"),null}SubgroupExclusiveAdd(e,t){return console.error("TODO: subgroupExclusiveAdd"),null}SubgroupInclusiveAdd(e,t){return console.error("TODO: subgroupInclusiveAdd"),null}SubgroupAll(e,t){return console.error("TODO: subgroupAll"),null}SubgroupAnd(e,t){return console.error("TODO: subgroupAnd"),null}SubgroupAny(e,t){return console.error("TODO: subgroupAny"),null}SubgroupBallot(e,t){return console.error("TODO: subgroupBallot"),null}SubgroupBroadcast(e,t){return console.error("TODO: subgroupBroadcast"),null}SubgroupBroadcastFirst(e,t){return console.error("TODO: subgroupBroadcastFirst"),null}SubgroupElect(e,t){return console.error("TODO: subgroupElect"),null}SubgroupMax(e,t){return console.error("TODO: subgroupMax"),null}SubgroupMin(e,t){return console.error("TODO: subgroupMin"),null}SubgroupMul(e,t){return console.error("TODO: subgroupMul"),null}SubgroupExclusiveMul(e,t){return console.error("TODO: subgroupExclusiveMul"),null}SubgroupInclusiveMul(e,t){return console.error("TODO: subgroupInclusiveMul"),null}SubgroupOr(e,t){return console.error("TODO: subgroupOr"),null}SubgroupShuffle(e,t){return console.error("TODO: subgroupShuffle"),null}SubgroupShuffleDown(e,t){return console.error("TODO: subgroupShuffleDown"),null}SubgroupShuffleUp(e,t){return console.error("TODO: subgroupShuffleUp"),null}SubgroupShuffleXor(e,t){return console.error("TODO: subgroupShuffleXor"),null}SubgroupXor(e,t){return console.error("TODO: subgroupXor"),null}QuadBroadcast(e,t){return console.error("TODO: quadBroadcast"),null}QuadSwapDiagonal(e,t){return console.error("TODO: quadSwapDiagonal"),null}QuadSwapX(e,t){return console.error("TODO: quadSwapX"),null}QuadSwapY(e,t){return console.error("TODO: quadSwapY"),null}},zt={vec2:2,vec2f:2,vec2i:2,vec2u:2,vec2b:2,vec2h:2,vec3:3,vec3f:3,vec3i:3,vec3u:3,vec3b:3,vec3h:3,vec4:4,vec4f:4,vec4i:4,vec4u:4,vec4b:4,vec4h:4},H={mat2x2:[2,2,4],mat2x2f:[2,2,4],mat2x2h:[2,2,4],mat2x3:[2,3,6],mat2x3f:[2,3,6],mat2x3h:[2,3,6],mat2x4:[2,4,8],mat2x4f:[2,4,8],mat2x4h:[2,4,8],mat3x2:[3,2,6],mat3x2f:[3,2,6],mat3x2h:[3,2,6],mat3x3:[3,3,9],mat3x3f:[3,3,9],mat3x3h:[3,3,9],mat3x4:[3,4,12],mat3x4f:[3,4,12],mat3x4h:[3,4,12],mat4x2:[4,2,8],mat4x2f:[4,2,8],mat4x2h:[4,2,8],mat4x3:[4,3,12],mat4x3f:[4,3,12],mat4x3h:[4,3,12],mat4x4:[4,4,16],mat4x4f:[4,4,16],mat4x4h:[4,4,16]},V=class extends on{constructor(e,t){var n;super(),this.ast=e??[],this.reflection=new Z,this.reflection.updateAST(this.ast),this.context=(n=t?.clone())!==null&&n!==void 0?n:new qe,this.builtins=new an(this),this.typeInfo={bool:this.getTypeInfo(x.bool),i32:this.getTypeInfo(x.i32),u32:this.getTypeInfo(x.u32),f32:this.getTypeInfo(x.f32),f16:this.getTypeInfo(x.f16),vec2f:this.getTypeInfo(g.vec2f),vec2u:this.getTypeInfo(g.vec2u),vec2i:this.getTypeInfo(g.vec2i),vec2h:this.getTypeInfo(g.vec2h),vec3f:this.getTypeInfo(g.vec3f),vec3u:this.getTypeInfo(g.vec3u),vec3i:this.getTypeInfo(g.vec3i),vec3h:this.getTypeInfo(g.vec3h),vec4f:this.getTypeInfo(g.vec4f),vec4u:this.getTypeInfo(g.vec4u),vec4i:this.getTypeInfo(g.vec4i),vec4h:this.getTypeInfo(g.vec4h),mat2x2f:this.getTypeInfo(g.mat2x2f),mat2x3f:this.getTypeInfo(g.mat2x3f),mat2x4f:this.getTypeInfo(g.mat2x4f),mat3x2f:this.getTypeInfo(g.mat3x2f),mat3x3f:this.getTypeInfo(g.mat3x3f),mat3x4f:this.getTypeInfo(g.mat3x4f),mat4x2f:this.getTypeInfo(g.mat4x2f),mat4x3f:this.getTypeInfo(g.mat4x3f),mat4x4f:this.getTypeInfo(g.mat4x4f)}}getVariableValue(e){var t,n;let r=(n=(t=this.context.getVariable(e))===null||t===void 0?void 0:t.value)!==null&&n!==void 0?n:null;if(r===null)return null;if(r instanceof d)return r.value;if(r instanceof p||r instanceof I)return Array.from(r.data);if(r instanceof D&&r.typeInfo instanceof se){if(r.typeInfo.format.name==="u32")return Array.from(new Uint32Array(r.buffer,r.offset,r.typeInfo.count));if(r.typeInfo.format.name==="i32")return Array.from(new Int32Array(r.buffer,r.offset,r.typeInfo.count));if(r.typeInfo.format.name==="f32")return Array.from(new Float32Array(r.buffer,r.offset,r.typeInfo.count))}return console.error(`Unsupported return variable type ${r.typeInfo.name}`),null}execute(e){(e=e??{}).constants&&this._setOverrides(e.constants,this.context),this._execStatements(this.ast,this.context)}dispatchWorkgroups(e,t,n,r){let s=this.context.clone();(r=r??{}).constants&&this._setOverrides(r.constants,s),this._execStatements(this.ast,s);let i=s.getFunction(e);if(!i)return void console.error(`Function ${e} not found`);if(typeof t=="number")t=[t,1,1];else{if(t.length===0)return void console.error("Invalid dispatch count");t.length===1?t=[t[0],1,1]:t.length===2?t=[t[0],t[1],1]:t.length>3&&(t=[t[0],t[1],t[2]])}let o=t[0],l=t[1],c=t[2],u=this.getTypeInfo("vec3u");s.setVariable("@num_workgroups",new p(t,u));for(let h in n)for(let b in n[h]){let y=n[h][b];s.variables.forEach(S=>{var E;let T=S.node;if(T?.attributes){let L=null,P=null;for(let k of T.attributes)k.name==="binding"?L=k.value:k.name==="group"&&(P=k.value);if(b==L&&h==P)if(y.texture!==void 0&&y.descriptor!==void 0){let k=new J(y.texture,this.getTypeInfo(T.type),y.descriptor,(E=y.texture.view)!==null&&E!==void 0?E:null);S.value=k}else y.uniform!==void 0?S.value=new D(y.uniform,this.getTypeInfo(T.type)):S.value=new D(y,this.getTypeInfo(T.type))}})}for(let h=0;h<c;++h)for(let b=0;b<l;++b)for(let y=0;y<o;++y)s.setVariable("@workgroup_id",new p([y,b,h],this.getTypeInfo("vec3u"))),this._dispatchWorkgroup(i,[y,b,h],s)}execStatement(e,t){if(e instanceof dt)return this.evalExpression(e.value,t);if(e instanceof _t){if(e.condition){let n=this.evalExpression(e.condition,t);if(!(n instanceof d))throw new Error("Invalid break-if condition");if(!n.value)return null}return V._breakObj}if(e instanceof gt)return V._continueObj;if(e instanceof me)this._let(e,t);else if(e instanceof ee)this._var(e,t);else if(e instanceof ke)this._const(e,t);else if(e instanceof ge)this._function(e,t);else{if(e instanceof pt)return this._if(e,t);if(e instanceof ht)return this._switch(e,t);if(e instanceof lt)return this._for(e,t);if(e instanceof at)return this._while(e,t);if(e instanceof ft)return this._loop(e,t);if(e instanceof Be){let n=t.clone();return n.currentFunctionName=t.currentFunctionName,this._execStatements(e.body,n)}if(e instanceof ut)this._assign(e,t);else if(e instanceof ct)this._increment(e,t);else{if(e instanceof Y)return null;if(e instanceof Ve){let n=e.name;t.getVariable(n)===null&&t.setVariable(n,new d(0,this.getTypeInfo("u32")))}else if(e instanceof Ue)this._call(e,t);else{if(e instanceof mt||e instanceof $e)return null;console.error("Invalid statement type.",e,`Line ${e.line}`)}}}return null}evalExpression(e,t){return e instanceof j?this._evalBinaryOp(e,t):e instanceof R?this._evalLiteral(e,t):e instanceof G?this._evalVariable(e,t):e instanceof He?this._evalCall(e,t):e instanceof K?this._evalCreate(e,t):e instanceof bt?this._evalConst(e,t):e instanceof xt?this._evalBitcast(e,t):e instanceof M?this._evalUnaryOp(e,t):(console.error("Invalid expression type",e,`Line ${e.line}`),null)}getTypeInfo(e){var t;if(e instanceof x){let r=this.reflection.getTypeInfo(e);if(r!==null)return r}let n=(t=this.typeInfo[e])!==null&&t!==void 0?t:null;return n!==null||(n=this.reflection.getTypeInfoByName(e)),n}_setOverrides(e,t){for(let n in e){let r=e[n],s=this.reflection.getOverrideInfo(n);s!==null?(s.type===null&&(s.type=this.getTypeInfo("u32")),s.type.name==="u32"||s.type.name==="i32"||s.type.name==="f32"||s.type.name==="f16"?t.setVariable(n,new d(r,s.type)):s.type.name==="bool"?t.setVariable(n,new d(r?1:0,s.type)):s.type.name==="vec2"||s.type.name==="vec3"||s.type.name==="vec4"||s.type.name==="vec2f"||s.type.name==="vec3f"||s.type.name==="vec4f"||s.type.name==="vec2i"||s.type.name==="vec3i"||s.type.name==="vec4i"||s.type.name==="vec2u"||s.type.name==="vec3u"||s.type.name==="vec4u"||s.type.name==="vec2h"||s.type.name==="vec3h"||s.type.name==="vec4h"?t.setVariable(n,new p(r,s.type)):console.error(`Invalid constant type for ${n}`)):console.error(`Override ${n} does not exist in the shader.`)}}_dispatchWorkgroup(e,t,n){let r=[1,1,1];for(let u of e.node.attributes)if(u.name==="workgroup_size"){if(u.value.length>0){let h=n.getVariableValue(u.value[0]);r[0]=h instanceof d?h.value:parseInt(u.value[0])}if(u.value.length>1){let h=n.getVariableValue(u.value[1]);r[1]=h instanceof d?h.value:parseInt(u.value[1])}if(u.value.length>2){let h=n.getVariableValue(u.value[2]);r[2]=h instanceof d?h.value:parseInt(u.value[2])}}let s=this.getTypeInfo("vec3u"),i=this.getTypeInfo("u32");n.setVariable("@workgroup_size",new p(r,s));let o=r[0],l=r[1],c=r[2];for(let u=0,h=0;u<c;++u)for(let b=0;b<l;++b)for(let y=0;y<o;++y,++h){let S=[y,b,u],E=[y+t[0]*r[0],b+t[1]*r[1],u+t[2]*r[2]];n.setVariable("@local_invocation_id",new p(S,s)),n.setVariable("@global_invocation_id",new p(E,s)),n.setVariable("@local_invocation_index",new d(h,i)),this._dispatchExec(e,n)}}_dispatchExec(e,t){for(let n of e.node.args)for(let r of n.attributes)if(r.name==="builtin"){let s=`@${r.value}`,i=t.getVariable(s);i!==void 0&&t.variables.set(n.name,i)}this._execStatements(e.node.body,t)}getVariableName(e,t){for(;e instanceof M;)e=e.right;return e instanceof G?e.name:(console.error("Unknown variable type",e,"Line",e.line),null)}_execStatements(e,t){for(let n of e){if(n instanceof Array){let s=t.clone(),i=this._execStatements(n,s);if(i)return i;continue}let r=this.execStatement(n,t);if(r)return r}return null}_call(e,t){let n=t.clone();n.currentFunctionName=e.name;let r=t.getFunction(e.name);if(r){for(let s=0;s<r.node.args.length;++s){let i=r.node.args[s],o=this.evalExpression(e.args[s],n);n.setVariable(i.name,o,i)}this._execStatements(r.node.body,n)}else e.isBuiltin?this._callBuiltinFunction(e,n):this.getTypeInfo(e.name)&&this._evalCreate(e,t)}_increment(e,t){let n=this.getVariableName(e.variable,t),r=t.getVariable(n);r?e.operator==="++"?r.value instanceof d?r.value.value++:console.error(`Variable ${n} is not a scalar. Line ${e.line}`):e.operator==="--"?r.value instanceof d?r.value.value--:console.error(`Variable ${n} is not a scalar. Line ${e.line}`):console.error(`Unknown increment operator ${e.operator}. Line ${e.line}`):console.error(`Variable ${n} not found. Line ${e.line}`)}_getVariableData(e,t){if(e instanceof G){let n=this.getVariableName(e,t),r=t.getVariable(n);return r===null?(console.error(`Variable ${n} not found. Line ${e.line}`),null):r.value.getSubData(this,e.postfix,t)}if(e instanceof M){if(e.operator==="*"){let n=this._getVariableData(e.right,t);return n instanceof ue?n.reference.getSubData(this,e.postfix,t):(console.error(`Variable ${e.right} is not a pointer. Line ${e.line}`),null)}if(e.operator==="&"){let n=this._getVariableData(e.right,t);return new ue(n)}}return null}_assign(e,t){let n=null,r="<var>",s=null;if(e.variable instanceof M){let l=this._getVariableData(e.variable,t),c=this.evalExpression(e.value,t),u=e.operator;if(u==="="){if(l instanceof d||l instanceof p||l instanceof I){if(c instanceof d||c instanceof p||c instanceof I&&l.data.length===c.data.length)return void l.data.set(c.data);console.error(`Invalid assignment. Line ${e.line}`)}else if(l instanceof D&&c instanceof D&&l.buffer.byteLength-l.offset>=c.buffer.byteLength-c.offset)return void(l.buffer.byteLength%4==0?new Uint32Array(l.buffer,l.offset,l.typeInfo.size/4).set(new Uint32Array(c.buffer,c.offset,c.typeInfo.size/4)):new Uint8Array(l.buffer,l.offset,l.typeInfo.size).set(new Uint8Array(c.buffer,c.offset,c.typeInfo.size)));return console.error(`Invalid assignment. Line ${e.line}`),null}if(u==="+=")return l instanceof d||l instanceof p||l instanceof I?c instanceof d||c instanceof p||c instanceof I?void l.data.set(c.data.map((h,b)=>l.data[b]+h)):void console.error(`Invalid assignment . Line ${e.line}`):void console.error(`Invalid assignment. Line ${e.line}`);if(u==="-=")return(l instanceof d||l instanceof p||l instanceof I)&&(c instanceof d||c instanceof p||c instanceof I)?void l.data.set(c.data.map((h,b)=>l.data[b]-h)):void console.error(`Invalid assignment. Line ${e.line}`)}if(e.variable instanceof M){if(e.variable.operator==="*"){r=this.getVariableName(e.variable.right,t);let l=t.getVariable(r);if(!(l&&l.value instanceof ue))return void console.error(`Variable ${r} is not a pointer. Line ${e.line}`);n=l.value.reference;let c=e.variable.postfix;if(!c){let u=e.variable.right;for(;u instanceof M;){if(u.postfix){c=u.postfix;break}u=u.right}}c&&(n=n.getSubData(this,c,t))}}else{s=e.variable.postfix,r=this.getVariableName(e.variable,t);let l=t.getVariable(r);if(l===null)return void console.error(`Variable ${r} not found. Line ${e.line}`);n=l.value}if(n instanceof ue&&(n=n.reference),n===null)return void console.error(`Variable ${r} not found. Line ${e.line}`);let i=this.evalExpression(e.value,t),o=e.operator;if(o==="=")if(n instanceof D)n.setDataValue(this,i,s,t);else if(s){if(!(n instanceof p||n instanceof I))return void console.error(`Variable ${r} is not a vector or matrix. Line ${e.line}`);if(s instanceof fe){let l=this.evalExpression(s.index,t).value;if(n instanceof p){if(!(i instanceof d))return void console.error(`Invalid assignment to ${r}. Line ${e.line}`);n.data[l]=i.value}else{if(!(n instanceof I))return void console.error(`Invalid assignment to ${r}. Line ${e.line}`);{let c=this.evalExpression(s.index,t).value;if(c<0)return void console.error(`Invalid assignment to ${r}. Line ${e.line}`);if(!(i instanceof p))return void console.error(`Invalid assignment to ${r}. Line ${e.line}`);{let u=n.typeInfo.getTypeName();if(u==="mat2x2"||u==="mat2x2f"||u==="mat2x2h"){if(!(c<2&&i.data.length===2))return void console.error(`Invalid assignment to ${r}. Line ${e.line}`);n.data[2*c]=i.data[0],n.data[2*c+1]=i.data[1]}else if(u==="mat2x3"||u==="mat2x3f"||u==="mat2x3h"){if(!(c<2&&i.data.length===3))return void console.error(`Invalid assignment to ${r}. Line ${e.line}`);n.data[3*c]=i.data[0],n.data[3*c+1]=i.data[1],n.data[3*c+2]=i.data[2]}else if(u==="mat2x4"||u==="mat2x4f"||u==="mat2x4h"){if(!(c<2&&i.data.length===4))return void console.error(`Invalid assignment to ${r}. Line ${e.line}`);n.data[4*c]=i.data[0],n.data[4*c+1]=i.data[1],n.data[4*c+2]=i.data[2],n.data[4*c+3]=i.data[3]}else if(u==="mat3x2"||u==="mat3x2f"||u==="mat3x2h"){if(!(c<3&&i.data.length===2))return void console.error(`Invalid assignment to ${r}. Line ${e.line}`);n.data[2*c]=i.data[0],n.data[2*c+1]=i.data[1]}else if(u==="mat3x3"||u==="mat3x3f"||u==="mat3x3h"){if(!(c<3&&i.data.length===3))return void console.error(`Invalid assignment to ${r}. Line ${e.line}`);n.data[3*c]=i.data[0],n.data[3*c+1]=i.data[1],n.data[3*c+2]=i.data[2]}else if(u==="mat3x4"||u==="mat3x4f"||u==="mat3x4h"){if(!(c<3&&i.data.length===4))return void console.error(`Invalid assignment to ${r}. Line ${e.line}`);n.data[4*c]=i.data[0],n.data[4*c+1]=i.data[1],n.data[4*c+2]=i.data[2],n.data[4*c+3]=i.data[3]}else if(u==="mat4x2"||u==="mat4x2f"||u==="mat4x2h"){if(!(c<4&&i.data.length===2))return void console.error(`Invalid assignment to ${r}. Line ${e.line}`);n.data[2*c]=i.data[0],n.data[2*c+1]=i.data[1]}else if(u==="mat4x3"||u==="mat4x3f"||u==="mat4x3h"){if(!(c<4&&i.data.length===3))return void console.error(`Invalid assignment to ${r}. Line ${e.line}`);n.data[3*c]=i.data[0],n.data[3*c+1]=i.data[1],n.data[3*c+2]=i.data[2]}else{if(u!=="mat4x4"&&u!=="mat4x4f"&&u!=="mat4x4h")return void console.error(`Invalid assignment to ${r}. Line ${e.line}`);if(!(c<4&&i.data.length===4))return void console.error(`Invalid assignment to ${r}. Line ${e.line}`);n.data[4*c]=i.data[0],n.data[4*c+1]=i.data[1],n.data[4*c+2]=i.data[2],n.data[4*c+3]=i.data[3]}}}}}else if(s instanceof ae){let l=s.value;if(!(n instanceof p))return void console.error(`Invalid assignment to ${l}. Variable ${r} is not a vector. Line ${e.line}`);if(i instanceof d){if(l.length>1)return void console.error(`Invalid assignment to ${l} for variable ${r}. Line ${e.line}`);if(l==="x")n.data[0]=i.value;else if(l==="y"){if(n.data.length<2)return void console.error(`Invalid assignment to ${l} for variable ${r}. Line ${e.line}`);n.data[1]=i.value}else if(l==="z"){if(n.data.length<3)return void console.error(`Invalid assignment to ${l} for variable ${r}. Line ${e.line}`);n.data[2]=i.value}else if(l==="w"){if(n.data.length<4)return void console.error(`Invalid assignment to ${l} for variable ${r}. Line ${e.line}`);n.data[3]=i.value}}else{if(!(i instanceof p))return void console.error(`Invalid assignment to ${r}. Line ${e.line}`);if(l.length!==i.data.length)return void console.error(`Invalid assignment to ${l} for variable ${r}. Line ${e.line}`);for(let c=0;c<l.length;++c){let u=l[c];if(u==="x"||u==="r")n.data[0]=i.data[c];else if(u==="y"||u==="g"){if(i.data.length<2)return void console.error(`Invalid assignment to ${u} for variable ${r}. Line ${e.line}`);n.data[1]=i.data[c]}else if(u==="z"||u==="b"){if(i.data.length<3)return void console.error(`Invalid assignment to ${u} for variable ${r}. Line ${e.line}`);n.data[2]=i.data[c]}else{if(u!=="w"&&u!=="a")return void console.error(`Invalid assignment to ${u} for variable ${r}. Line ${e.line}`);if(i.data.length<4)return void console.error(`Invalid assignment to ${u} for variable ${r}. Line ${e.line}`);n.data[3]=i.data[c]}}}}}else n instanceof d&&i instanceof d?n.value=i.value:n instanceof p&&i instanceof p||n instanceof I&&i instanceof I?n.data.set(i.data):console.error(`Invalid assignment to ${r}. Line ${e.line}`);else{let l=n.getSubData(this,s,t);if(l instanceof p&&i instanceof d){let c=l.data,u=i.value;if(o==="+=")for(let h=0;h<c.length;++h)c[h]+=u;else if(o==="-=")for(let h=0;h<c.length;++h)c[h]-=u;else if(o==="*=")for(let h=0;h<c.length;++h)c[h]*=u;else if(o==="/=")for(let h=0;h<c.length;++h)c[h]/=u;else if(o==="%=")for(let h=0;h<c.length;++h)c[h]%=u;else if(o==="&=")for(let h=0;h<c.length;++h)c[h]&=u;else if(o==="|=")for(let h=0;h<c.length;++h)c[h]|=u;else if(o==="^=")for(let h=0;h<c.length;++h)c[h]^=u;else if(o==="<<=")for(let h=0;h<c.length;++h)c[h]<<=u;else if(o===">>=")for(let h=0;h<c.length;++h)c[h]>>=u;else console.error(`Invalid operator ${o}. Line ${e.line}`)}else if(l instanceof p&&i instanceof p){let c=l.data,u=i.data;if(c.length!==u.length)return void console.error(`Vector length mismatch. Line ${e.line}`);if(o==="+=")for(let h=0;h<c.length;++h)c[h]+=u[h];else if(o==="-=")for(let h=0;h<c.length;++h)c[h]-=u[h];else if(o==="*=")for(let h=0;h<c.length;++h)c[h]*=u[h];else if(o==="/=")for(let h=0;h<c.length;++h)c[h]/=u[h];else if(o==="%=")for(let h=0;h<c.length;++h)c[h]%=u[h];else if(o==="&=")for(let h=0;h<c.length;++h)c[h]&=u[h];else if(o==="|=")for(let h=0;h<c.length;++h)c[h]|=u[h];else if(o==="^=")for(let h=0;h<c.length;++h)c[h]^=u[h];else if(o==="<<=")for(let h=0;h<c.length;++h)c[h]<<=u[h];else if(o===">>=")for(let h=0;h<c.length;++h)c[h]>>=u[h];else console.error(`Invalid operator ${o}. Line ${e.line}`)}else{if(!(l instanceof d&&i instanceof d))return void console.error(`Invalid type for ${e.operator} operator. Line ${e.line}`);o==="+="?l.value+=i.value:o==="-="?l.value-=i.value:o==="*="?l.value*=i.value:o==="/="?l.value/=i.value:o==="%="?l.value%=i.value:o==="&="?l.value&=i.value:o==="|="?l.value|=i.value:o==="^="?l.value^=i.value:o==="<<="?l.value<<=i.value:o===">>="?l.value>>=i.value:console.error(`Invalid operator ${o}. Line ${e.line}`)}n instanceof D&&n.setDataValue(this,l,s,t)}}_function(e,t){let n=new ze(e);t.functions.set(e.name,n)}_const(e,t){let n=null;e.value!==null&&(n=this.evalExpression(e.value,t)),t.createVariable(e.name,n,e)}_let(e,t){let n=null;if(e.value!==null){if(n=this.evalExpression(e.value,t),n===null)return void console.error(`Invalid value for variable ${e.name}. Line ${e.line}`);e.value instanceof M||(n=n.clone())}else{let r=e.type.name;if(r==="f32"||r==="i32"||r==="u32"||r==="bool"||r==="f16"||r==="vec2"||r==="vec3"||r==="vec4"||r==="vec2f"||r==="vec3f"||r==="vec4f"||r==="vec2i"||r==="vec3i"||r==="vec4i"||r==="vec2u"||r==="vec3u"||r==="vec4u"||r==="vec2h"||r==="vec3h"||r==="vec4h"||r==="vec2b"||r==="vec3b"||r==="vec4b"||r==="mat2x2"||r==="mat2x3"||r==="mat2x4"||r==="mat3x2"||r==="mat3x3"||r==="mat3x4"||r==="mat4x2"||r==="mat4x3"||r==="mat4x4"||r==="mat2x2f"||r==="mat2x3f"||r==="mat2x4f"||r==="mat3x2f"||r==="mat3x3f"||r==="mat3x4f"||r==="mat4x2f"||r==="mat4x3f"||r==="mat4x4f"||r==="mat2x2h"||r==="mat2x3h"||r==="mat2x4h"||r==="mat3x2h"||r==="mat3x3h"||r==="mat3x4h"||r==="mat4x2h"||r==="mat4x3h"||r==="mat4x4h"||r==="array"){let s=new K(e.type,[]);n=this._evalCreate(s,t)}}t.createVariable(e.name,n,e)}_var(e,t){let n=null;if(e.value!==null){if(n=this.evalExpression(e.value,t),n===null)return void console.error(`Invalid value for variable ${e.name}. Line ${e.line}`);e.value instanceof M||(n=n.clone())}else{if(e.type===null)return void console.error(`Variable ${e.name} has no type. Line ${e.line}`);let r=e.type.name;if(r==="f32"||r==="i32"||r==="u32"||r==="bool"||r==="f16"||r==="vec2"||r==="vec3"||r==="vec4"||r==="vec2f"||r==="vec3f"||r==="vec4f"||r==="vec2i"||r==="vec3i"||r==="vec4i"||r==="vec2u"||r==="vec3u"||r==="vec4u"||r==="vec2h"||r==="vec3h"||r==="vec4h"||r==="vec2b"||r==="vec3b"||r==="vec4b"||r==="mat2x2"||r==="mat2x3"||r==="mat2x4"||r==="mat3x2"||r==="mat3x3"||r==="mat3x4"||r==="mat4x2"||r==="mat4x3"||r==="mat4x4"||r==="mat2x2f"||r==="mat2x3f"||r==="mat2x4f"||r==="mat3x2f"||r==="mat3x3f"||r==="mat3x4f"||r==="mat4x2f"||r==="mat4x3f"||r==="mat4x4f"||r==="mat2x2h"||r==="mat2x3h"||r==="mat2x4h"||r==="mat3x2h"||r==="mat3x3h"||r==="mat3x4h"||r==="mat4x2h"||r==="mat4x3h"||r==="mat4x4h"||e.type instanceof _e||e.type instanceof Y||e.type instanceof g){let s=new K(e.type,[]);n=this._evalCreate(s,t)}}t.createVariable(e.name,n,e)}_switch(e,t){t=t.clone();let n=this.evalExpression(e.condition,t);if(!(n instanceof d))return console.error(`Invalid if condition. Line ${e.line}`),null;let r=null;for(let s of e.cases)if(s instanceof kt)for(let i of s.selectors){if(i instanceof Ie){r=s;continue}let o=this.evalExpression(i,t);if(!(o instanceof d))return console.error(`Invalid case selector. Line ${e.line}`),null;if(o.value===n.value)return this._execStatements(s.body,t)}else s instanceof It&&(r=s);return r?this._execStatements(r.body,t):null}_if(e,t){t=t.clone();let n=this.evalExpression(e.condition,t);if(!(n instanceof d))return console.error(`Invalid if condition. Line ${e.line}`),null;if(n.value)return this._execStatements(e.body,t);for(let r of e.elseif){let s=this.evalExpression(r.condition,t);if(!(s instanceof d))return console.error(`Invalid if condition. Line ${e.line}`),null;if(s.value)return this._execStatements(r.body,t)}return e.else?this._execStatements(e.else,t):null}_getScalarValue(e){return e instanceof d?e.value:(console.error("Expected scalar value.",e),0)}_for(e,t){for(t=t.clone(),this.execStatement(e.init,t);this._getScalarValue(this.evalExpression(e.condition,t));){let n=this._execStatements(e.body,t);if(n===V._breakObj)break;if(n!==null&&n!==V._continueObj)return n;this.execStatement(e.increment,t)}return null}_loop(e,t){for(t=t.clone();;){let n=this._execStatements(e.body,t);if(n===V._breakObj)break;if(n===V._continueObj){if(e.continuing&&this._execStatements(e.continuing.body,t)===V._breakObj)break}else if(n!==null)return n}return null}_while(e,t){for(t=t.clone();this._getScalarValue(this.evalExpression(e.condition,t));){let n=this._execStatements(e.body,t);if(n===V._breakObj)break;if(n!==V._continueObj&&n!==null)return n}return null}_evalBitcast(e,t){let n=this.evalExpression(e.value,t),r=e.type;if(n instanceof d){let s=jn(n.value,n.typeInfo.name,r.name);return new d(s,this.getTypeInfo(r))}if(n instanceof p){let s=n.typeInfo.getTypeName(),i="";if(s.endsWith("f"))i="f32";else if(s.endsWith("i"))i="i32";else if(s.endsWith("u"))i="u32";else if(s.endsWith("b"))i="bool";else{if(!s.endsWith("h"))return console.error(`Unknown vector type ${s}. Line ${e.line}`),null;i="f16"}let o=r.getTypeName(),l="";if(o.endsWith("f"))l="f32";else if(o.endsWith("i"))l="i32";else if(o.endsWith("u"))l="u32";else if(o.endsWith("b"))l="bool";else{if(!o.endsWith("h"))return console.error(`Unknown vector type ${l}. Line ${e.line}`),null;l="f16"}let c=function(u,h,b){if(h===b)return u;let y=new Array(u.length);for(let S=0;S<u.length;S++)y[S]=jn(u[S],h,b);return y}(Array.from(n.data),i,l);return new p(c,this.getTypeInfo(r))}return console.error(`TODO: bitcast for ${n.typeInfo.name}. Line ${e.line}`),null}_evalConst(e,t){return t.getVariableValue(e.name).clone().getSubData(this,e.postfix,t)}_evalCreate(e,t){var n;if(e instanceof K){if(e.type===null)return Ge.void;switch(e.type.getTypeName()){case"bool":case"i32":case"u32":case"f32":case"f16":return this._callConstructorValue(e,t);case"vec2":case"vec3":case"vec4":case"vec2f":case"vec3f":case"vec4f":case"vec2h":case"vec3h":case"vec4h":case"vec2i":case"vec3i":case"vec4i":case"vec2u":case"vec3u":case"vec4u":case"vec2b":case"vec3b":case"vec4b":return this._callConstructorVec(e,t);case"mat2x2":case"mat2x2f":case"mat2x2h":case"mat2x3":case"mat2x3f":case"mat2x3h":case"mat2x4":case"mat2x4f":case"mat2x4h":case"mat3x2":case"mat3x2f":case"mat3x2h":case"mat3x3":case"mat3x3f":case"mat3x3h":case"mat3x4":case"mat3x4f":case"mat3x4h":case"mat4x2":case"mat4x2f":case"mat4x2h":case"mat4x3":case"mat4x3f":case"mat4x3h":case"mat4x4":case"mat4x4f":case"mat4x4h":return this._callConstructorMatrix(e,t)}}let r=e instanceof K?e.type.name:e.name,s=e instanceof K?this.getTypeInfo(e.type):this.getTypeInfo(e.name);if(s===null)return console.error(`Unknown type ${r}. Line ${e.line}`),null;if(s.size===0)return null;let i=new D(new ArrayBuffer(s.size),s,0);if(s instanceof re){if(e.args)for(let o=0;o<e.args.length;++o){let l=s.members[o],c=e.args[o],u=this.evalExpression(c,t);i.setData(this,u,l.type,l.offset,t)}}else if(s instanceof se){let o=0;if(e.args)for(let l=0;l<e.args.length;++l){let c=e.args[l],u=this.evalExpression(c,t);s.format===null&&(((n=u.typeInfo)===null||n===void 0?void 0:n.name)==="x32"?s.format=this.getTypeInfo("i32"):s.format=u.typeInfo),i.setData(this,u,s.format,o,t),o+=s.stride}}else console.error(`Unknown type "${r}". Line ${e.line}`);return e instanceof K?i.getSubData(this,e.postfix,t):i}_evalLiteral(e,t){let n=this.getTypeInfo(e.type),r=n.name;return r==="x32"||r==="u32"||r==="f32"||r==="f16"||r==="i32"||r==="bool"?new d(e.scalarValue,n):r==="vec2"||r==="vec3"||r==="vec4"||r==="vec2f"||r==="vec3f"||r==="vec4f"||r==="vec2h"||r==="vec3h"||r==="vec4h"||r==="vec2i"||r==="vec3i"||r==="vec4i"||r==="vec2u"||r==="vec3u"||r==="vec4u"?this._callConstructorVec(e,t):r==="mat2x2"||r==="mat2x3"||r==="mat2x4"||r==="mat3x2"||r==="mat3x3"||r==="mat3x4"||r==="mat4x2"||r==="mat4x3"||r==="mat4x4"||r==="mat2x2f"||r==="mat2x3f"||r==="mat2x4f"||r==="mat3x2f"||r==="mat3x3f"||r==="mat3x4f"||r==="mat4x2f"||r==="mat4x3f"||r==="mat4x4f"||r==="mat2x2h"||r==="mat2x3h"||r==="mat2x4h"||r==="mat3x2h"||r==="mat3x3h"||r==="mat3x4h"||r==="mat4x2h"||r==="mat4x3h"||r==="mat4x4h"?this._callConstructorMatrix(e,t):e.value}_evalVariable(e,t){let n=t.getVariableValue(e.name);return n===null?n:n.getSubData(this,e.postfix,t)}_maxFormatTypeInfo(e){let t=e[0];if(t.name==="f32")return t;for(let n=1;n<e.length;++n){let r=V._priority.get(t.name);V._priority.get(e[n].name)<r&&(t=e[n])}return t.name==="x32"?this.getTypeInfo("i32"):t}_evalUnaryOp(e,t){let n=this.evalExpression(e.right,t);if(e.operator==="&")return new ue(n);if(e.operator==="*")return n instanceof ue?n.reference.getSubData(this,e.postfix,t):(console.error(`Invalid dereference. Line ${e.line}`),null);let r=n instanceof d?n.value:n instanceof p?Array.from(n.data):null;switch(e.operator){case"+":{if(w(r)){let o=r.map((l,c)=>+l);return new p(o,n.typeInfo)}let s=r,i=this._maxFormatTypeInfo([n.typeInfo,n.typeInfo]);return new d(+s,i)}case"-":{if(w(r)){let o=r.map((l,c)=>-l);return new p(o,n.typeInfo)}let s=r,i=this._maxFormatTypeInfo([n.typeInfo,n.typeInfo]);return new d(-s,i)}case"!":{if(w(r)){let o=r.map((l,c)=>l?0:1);return new p(o,n.typeInfo)}let s=r,i=this._maxFormatTypeInfo([n.typeInfo,n.typeInfo]);return new d(s?0:1,i)}case"~":{if(w(r)){let o=r.map((l,c)=>~l);return new p(o,n.typeInfo)}let s=r,i=this._maxFormatTypeInfo([n.typeInfo,n.typeInfo]);return new d(~s,i)}}return console.error(`Invalid unary operator ${e.operator}. Line ${e.line}`),null}_evalBinaryOp(e,t){let n=this.evalExpression(e.left,t),r=this.evalExpression(e.right,t),s=n instanceof d?n.value:n instanceof p||n instanceof I?Array.from(n.data):null,i=r instanceof d?r.value:r instanceof p||r instanceof I?Array.from(r.data):null;switch(e.operator){case"+":{if(w(s)&&w(i)){let u=s,h=i;if(u.length!==h.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let b=u.map((y,S)=>y+h[S]);return new p(b,n.typeInfo)}if(w(s)){let u=i,h=s.map((b,y)=>b+u);return new p(h,n.typeInfo)}if(w(i)){let u=s,h=i.map((b,y)=>u+b);return new p(h,r.typeInfo)}let o=s,l=i,c=this._maxFormatTypeInfo([n.typeInfo,r.typeInfo]);return new d(o+l,c)}case"-":{if(w(s)&&w(i)){let u=s,h=i;if(u.length!==h.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let b=u.map((y,S)=>y-h[S]);return new p(b,n.typeInfo)}if(w(s)){let u=i,h=s.map((b,y)=>b-u);return new p(h,n.typeInfo)}if(w(i)){let u=s,h=i.map((b,y)=>u-b);return new p(h,r.typeInfo)}let o=s,l=i,c=this._maxFormatTypeInfo([n.typeInfo,r.typeInfo]);return new d(o-l,c)}case"*":{if(w(s)&&w(i)){let u=s,h=i;if(n instanceof I&&r instanceof I){let b=function(T,L,P,k){if(H[L.name]===void 0||H[k.name]===void 0)return null;let A=H[L.name][0],B=H[L.name][1],O=H[k.name][0];if(A!==H[k.name][1])return null;let oe=new Array(O*B);for(let X=0;X<B;X++)for(let U=0;U<O;U++){let he=0;for(let Le=0;Le<A;Le++)he+=T[Le*B+X]*P[U*A+Le];oe[X*O+U]=he}return oe}(u,n.typeInfo,h,r.typeInfo);if(b===null)return console.error(`Matrix multiplication failed. Line ${e.line}.`),null;let y=H[r.typeInfo.name][0],S=H[n.typeInfo.name][1],E=this.getTypeInfo(`mat${y}x${S}f`);return new I(b,E)}if(n instanceof I&&r instanceof p){let b=function(y,S,E,T){if(H[S.name]===void 0||zt[T.name]===void 0)return null;let L=H[S.name][0],P=H[S.name][1];if(L!==E.length)return null;let k=new Array(P);for(let A=0;A<P;A++){let B=0;for(let O=0;O<L;O++)B+=y[O*P+A]*E[O];k[A]=B}return k}(u,n.typeInfo,h,r.typeInfo);return b===null?(console.error(`Matrix vector multiplication failed. Line ${e.line}.`),null):new p(b,r.typeInfo)}if(n instanceof p&&r instanceof I){let b=function(y,S,E,T){if(zt[S.name]===void 0||H[T.name]===void 0)return null;let L=H[T.name][0],P=H[T.name][1];if(P!==y.length)return null;let k=[];for(let A=0;A<L;A++){let B=0;for(let O=0;O<P;O++)B+=y[O]*E[O*L+A];k[A]=B}return k}(u,n.typeInfo,h,r.typeInfo);return b===null?(console.error(`Matrix vector multiplication failed. Line ${e.line}.`),null):new p(b,n.typeInfo)}{if(u.length!==h.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let b=u.map((y,S)=>y*h[S]);return new p(b,n.typeInfo)}}if(w(s)){let u=i,h=s.map((b,y)=>b*u);return n instanceof I?new I(h,n.typeInfo):new p(h,n.typeInfo)}if(w(i)){let u=s,h=i.map((b,y)=>u*b);return r instanceof I?new I(h,r.typeInfo):new p(h,r.typeInfo)}let o=s,l=i,c=this._maxFormatTypeInfo([n.typeInfo,r.typeInfo]);return new d(o*l,c)}case"%":{if(w(s)&&w(i)){let u=s,h=i;if(u.length!==h.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let b=u.map((y,S)=>y%h[S]);return new p(b,n.typeInfo)}if(w(s)){let u=i,h=s.map((b,y)=>b%u);return new p(h,n.typeInfo)}if(w(i)){let u=s,h=i.map((b,y)=>u%b);return new p(h,r.typeInfo)}let o=s,l=i,c=this._maxFormatTypeInfo([n.typeInfo,r.typeInfo]);return new d(o%l,c)}case"/":{if(w(s)&&w(i)){let u=s,h=i;if(u.length!==h.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let b=u.map((y,S)=>y/h[S]);return new p(b,n.typeInfo)}if(w(s)){let u=i,h=s.map((b,y)=>b/u);return new p(h,n.typeInfo)}if(w(i)){let u=s,h=i.map((b,y)=>u/b);return new p(h,r.typeInfo)}let o=s,l=i,c=this._maxFormatTypeInfo([n.typeInfo,r.typeInfo]);return new d(o/l,c)}case"&":{if(w(s)&&w(i)){let u=s,h=i;if(u.length!==h.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let b=u.map((y,S)=>y&h[S]);return new p(b,n.typeInfo)}if(w(s)){let u=i,h=s.map((b,y)=>b&u);return new p(h,n.typeInfo)}if(w(i)){let u=s,h=i.map((b,y)=>u&b);return new p(h,r.typeInfo)}let o=s,l=i,c=this._maxFormatTypeInfo([n.typeInfo,r.typeInfo]);return new d(o&l,c)}case"|":{if(w(s)&&w(i)){let u=s,h=i;if(u.length!==h.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let b=u.map((y,S)=>y|h[S]);return new p(b,n.typeInfo)}if(w(s)){let u=i,h=s.map((b,y)=>b|u);return new p(h,n.typeInfo)}if(w(i)){let u=s,h=i.map((b,y)=>u|b);return new p(h,r.typeInfo)}let o=s,l=i,c=this._maxFormatTypeInfo([n.typeInfo,r.typeInfo]);return new d(o|l,c)}case"^":{if(w(s)&&w(i)){let u=s,h=i;if(u.length!==h.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let b=u.map((y,S)=>y^h[S]);return new p(b,n.typeInfo)}if(w(s)){let u=i,h=s.map((b,y)=>b^u);return new p(h,n.typeInfo)}if(w(i)){let u=s,h=i.map((b,y)=>u^b);return new p(h,r.typeInfo)}let o=s,l=i,c=this._maxFormatTypeInfo([n.typeInfo,r.typeInfo]);return new d(o^l,c)}case"<<":{if(w(s)&&w(i)){let u=s,h=i;if(u.length!==h.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let b=u.map((y,S)=>y<<h[S]);return new p(b,n.typeInfo)}if(w(s)){let u=i,h=s.map((b,y)=>b<<u);return new p(h,n.typeInfo)}if(w(i)){let u=s,h=i.map((b,y)=>u<<b);return new p(h,r.typeInfo)}let o=s,l=i,c=this._maxFormatTypeInfo([n.typeInfo,r.typeInfo]);return new d(o<<l,c)}case">>":{if(w(s)&&w(i)){let u=s,h=i;if(u.length!==h.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let b=u.map((y,S)=>y>>h[S]);return new p(b,n.typeInfo)}if(w(s)){let u=i,h=s.map((b,y)=>b>>u);return new p(h,n.typeInfo)}if(w(i)){let u=s,h=i.map((b,y)=>u>>b);return new p(h,r.typeInfo)}let o=s,l=i,c=this._maxFormatTypeInfo([n.typeInfo,r.typeInfo]);return new d(o>>l,c)}case">":if(w(s)&&w(i)){let o=s,l=i;if(o.length!==l.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let c=o.map((u,h)=>u>l[h]?1:0);return new p(c,n.typeInfo)}if(w(s)){let o=i,l=s.map((c,u)=>c>o?1:0);return new p(l,n.typeInfo)}if(w(i)){let o=s,l=i.map((c,u)=>o>c?1:0);return new p(l,r.typeInfo)}return new d(s>i?1:0,this.getTypeInfo("bool"));case"<":if(w(s)&&w(i)){let o=s,l=i;if(o.length!==l.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let c=o.map((u,h)=>u<l[h]?1:0);return new p(c,n.typeInfo)}if(w(s)){let o=i,l=s.map((c,u)=>c<o?1:0);return new p(l,n.typeInfo)}if(w(i)){let o=s,l=i.map((c,u)=>o<c?1:0);return new p(l,r.typeInfo)}return new d(s<i?1:0,this.getTypeInfo("bool"));case"==":if(w(s)&&w(i)){let o=s,l=i;if(o.length!==l.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let c=o.map((u,h)=>u===l[h]?1:0);return new p(c,n.typeInfo)}if(w(s)){let o=i,l=s.map((c,u)=>c==o?1:0);return new p(l,n.typeInfo)}if(w(i)){let o=s,l=i.map((c,u)=>o==c?1:0);return new p(l,r.typeInfo)}return new d(s===i?1:0,this.getTypeInfo("bool"));case"!=":if(w(s)&&w(i)){let o=s,l=i;if(o.length!==l.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let c=o.map((u,h)=>u!==l[h]?1:0);return new p(c,n.typeInfo)}if(w(s)){let o=i,l=s.map((c,u)=>c!==o?1:0);return new p(l,n.typeInfo)}if(w(i)){let o=s,l=i.map((c,u)=>o!==c?1:0);return new p(l,r.typeInfo)}return new d(s!==i?1:0,this.getTypeInfo("bool"));case">=":if(w(s)&&w(i)){let o=s,l=i;if(o.length!==l.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let c=o.map((u,h)=>u>=l[h]?1:0);return new p(c,n.typeInfo)}if(w(s)){let o=i,l=s.map((c,u)=>c>=o?1:0);return new p(l,n.typeInfo)}if(w(i)){let o=s,l=i.map((c,u)=>o>=c?1:0);return new p(l,r.typeInfo)}return new d(s>=i?1:0,this.getTypeInfo("bool"));case"<=":if(w(s)&&w(i)){let o=s,l=i;if(o.length!==l.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let c=o.map((u,h)=>u<=l[h]?1:0);return new p(c,n.typeInfo)}if(w(s)){let o=i,l=s.map((c,u)=>c<=o?1:0);return new p(l,n.typeInfo)}if(w(i)){let o=s,l=i.map((c,u)=>o<=c?1:0);return new p(l,r.typeInfo)}return new d(s<=i?1:0,this.getTypeInfo("bool"));case"&&":if(w(s)&&w(i)){let o=s,l=i;if(o.length!==l.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let c=o.map((u,h)=>u&&l[h]?1:0);return new p(c,n.typeInfo)}if(w(s)){let o=i,l=s.map((c,u)=>c&&o?1:0);return new p(l,n.typeInfo)}if(w(i)){let o=s,l=i.map((c,u)=>o&&c?1:0);return new p(l,r.typeInfo)}return new d(s&&i?1:0,this.getTypeInfo("bool"));case"||":if(w(s)&&w(i)){let o=s,l=i;if(o.length!==l.length)return console.error(`Vector length mismatch. Line ${e.line}.`),null;let c=o.map((u,h)=>u||l[h]?1:0);return new p(c,n.typeInfo)}if(w(s)){let o=i,l=s.map((c,u)=>c||o?1:0);return new p(l,n.typeInfo)}if(w(i)){let o=s,l=i.map((c,u)=>o||c?1:0);return new p(l,r.typeInfo)}return new d(s||i?1:0,this.getTypeInfo("bool"))}return console.error(`Unknown operator ${e.operator}. Line ${e.line}`),null}_evalCall(e,t){if(e.cachedReturnValue!==null)return e.cachedReturnValue;let n=t.clone();n.currentFunctionName=e.name;let r=t.getFunction(e.name);if(!r)return e.isBuiltin?this._callBuiltinFunction(e,n):this.getTypeInfo(e.name)?this._evalCreate(e,t):(console.error(`Unknown function "${e.name}". Line ${e.line}`),null);for(let s=0;s<r.node.args.length;++s){let i=r.node.args[s],o=this.evalExpression(e.args[s],n);n.createVariable(i.name,o,i)}return this._execStatements(r.node.body,n)}_callBuiltinFunction(e,t){switch(e.name){case"all":return this.builtins.All(e,t);case"any":return this.builtins.Any(e,t);case"select":return this.builtins.Select(e,t);case"arrayLength":return this.builtins.ArrayLength(e,t);case"abs":return this.builtins.Abs(e,t);case"acos":return this.builtins.Acos(e,t);case"acosh":return this.builtins.Acosh(e,t);case"asin":return this.builtins.Asin(e,t);case"asinh":return this.builtins.Asinh(e,t);case"atan":return this.builtins.Atan(e,t);case"atanh":return this.builtins.Atanh(e,t);case"atan2":return this.builtins.Atan2(e,t);case"ceil":return this.builtins.Ceil(e,t);case"clamp":return this.builtins.Clamp(e,t);case"cos":return this.builtins.Cos(e,t);case"cosh":return this.builtins.Cosh(e,t);case"countLeadingZeros":return this.builtins.CountLeadingZeros(e,t);case"countOneBits":return this.builtins.CountOneBits(e,t);case"countTrailingZeros":return this.builtins.CountTrailingZeros(e,t);case"cross":return this.builtins.Cross(e,t);case"degrees":return this.builtins.Degrees(e,t);case"determinant":return this.builtins.Determinant(e,t);case"distance":return this.builtins.Distance(e,t);case"dot":return this.builtins.Dot(e,t);case"dot4U8Packed":return this.builtins.Dot4U8Packed(e,t);case"dot4I8Packed":return this.builtins.Dot4I8Packed(e,t);case"exp":return this.builtins.Exp(e,t);case"exp2":return this.builtins.Exp2(e,t);case"extractBits":return this.builtins.ExtractBits(e,t);case"faceForward":return this.builtins.FaceForward(e,t);case"firstLeadingBit":return this.builtins.FirstLeadingBit(e,t);case"firstTrailingBit":return this.builtins.FirstTrailingBit(e,t);case"floor":return this.builtins.Floor(e,t);case"fma":return this.builtins.Fma(e,t);case"fract":return this.builtins.Fract(e,t);case"frexp":return this.builtins.Frexp(e,t);case"insertBits":return this.builtins.InsertBits(e,t);case"inverseSqrt":return this.builtins.InverseSqrt(e,t);case"ldexp":return this.builtins.Ldexp(e,t);case"length":return this.builtins.Length(e,t);case"log":return this.builtins.Log(e,t);case"log2":return this.builtins.Log2(e,t);case"max":return this.builtins.Max(e,t);case"min":return this.builtins.Min(e,t);case"mix":return this.builtins.Mix(e,t);case"modf":return this.builtins.Modf(e,t);case"normalize":return this.builtins.Normalize(e,t);case"pow":return this.builtins.Pow(e,t);case"quantizeToF16":return this.builtins.QuantizeToF16(e,t);case"radians":return this.builtins.Radians(e,t);case"reflect":return this.builtins.Reflect(e,t);case"refract":return this.builtins.Refract(e,t);case"reverseBits":return this.builtins.ReverseBits(e,t);case"round":return this.builtins.Round(e,t);case"saturate":return this.builtins.Saturate(e,t);case"sign":return this.builtins.Sign(e,t);case"sin":return this.builtins.Sin(e,t);case"sinh":return this.builtins.Sinh(e,t);case"smoothStep":return this.builtins.SmoothStep(e,t);case"sqrt":return this.builtins.Sqrt(e,t);case"step":return this.builtins.Step(e,t);case"tan":return this.builtins.Tan(e,t);case"tanh":return this.builtins.Tanh(e,t);case"transpose":return this.builtins.Transpose(e,t);case"trunc":return this.builtins.Trunc(e,t);case"dpdx":return this.builtins.Dpdx(e,t);case"dpdxCoarse":return this.builtins.DpdxCoarse(e,t);case"dpdxFine":return this.builtins.DpdxFine(e,t);case"dpdy":return this.builtins.Dpdy(e,t);case"dpdyCoarse":return this.builtins.DpdyCoarse(e,t);case"dpdyFine":return this.builtins.DpdyFine(e,t);case"fwidth":return this.builtins.Fwidth(e,t);case"fwidthCoarse":return this.builtins.FwidthCoarse(e,t);case"fwidthFine":return this.builtins.FwidthFine(e,t);case"textureDimensions":return this.builtins.TextureDimensions(e,t);case"textureGather":return this.builtins.TextureGather(e,t);case"textureGatherCompare":return this.builtins.TextureGatherCompare(e,t);case"textureLoad":return this.builtins.TextureLoad(e,t);case"textureNumLayers":return this.builtins.TextureNumLayers(e,t);case"textureNumLevels":return this.builtins.TextureNumLevels(e,t);case"textureNumSamples":return this.builtins.TextureNumSamples(e,t);case"textureSample":return this.builtins.TextureSample(e,t);case"textureSampleBias":return this.builtins.TextureSampleBias(e,t);case"textureSampleCompare":return this.builtins.TextureSampleCompare(e,t);case"textureSampleCompareLevel":return this.builtins.TextureSampleCompareLevel(e,t);case"textureSampleGrad":return this.builtins.TextureSampleGrad(e,t);case"textureSampleLevel":return this.builtins.TextureSampleLevel(e,t);case"textureSampleBaseClampToEdge":return this.builtins.TextureSampleBaseClampToEdge(e,t);case"textureStore":return this.builtins.TextureStore(e,t);case"atomicLoad":return this.builtins.AtomicLoad(e,t);case"atomicStore":return this.builtins.AtomicStore(e,t);case"atomicAdd":return this.builtins.AtomicAdd(e,t);case"atomicSub":return this.builtins.AtomicSub(e,t);case"atomicMax":return this.builtins.AtomicMax(e,t);case"atomicMin":return this.builtins.AtomicMin(e,t);case"atomicAnd":return this.builtins.AtomicAnd(e,t);case"atomicOr":return this.builtins.AtomicOr(e,t);case"atomicXor":return this.builtins.AtomicXor(e,t);case"atomicExchange":return this.builtins.AtomicExchange(e,t);case"atomicCompareExchangeWeak":return this.builtins.AtomicCompareExchangeWeak(e,t);case"pack4x8snorm":return this.builtins.Pack4x8snorm(e,t);case"pack4x8unorm":return this.builtins.Pack4x8unorm(e,t);case"pack4xI8":return this.builtins.Pack4xI8(e,t);case"pack4xU8":return this.builtins.Pack4xU8(e,t);case"pack4x8Clamp":return this.builtins.Pack4x8Clamp(e,t);case"pack4xU8Clamp":return this.builtins.Pack4xU8Clamp(e,t);case"pack2x16snorm":return this.builtins.Pack2x16snorm(e,t);case"pack2x16unorm":return this.builtins.Pack2x16unorm(e,t);case"pack2x16float":return this.builtins.Pack2x16float(e,t);case"unpack4x8snorm":return this.builtins.Unpack4x8snorm(e,t);case"unpack4x8unorm":return this.builtins.Unpack4x8unorm(e,t);case"unpack4xI8":return this.builtins.Unpack4xI8(e,t);case"unpack4xU8":return this.builtins.Unpack4xU8(e,t);case"unpack2x16snorm":return this.builtins.Unpack2x16snorm(e,t);case"unpack2x16unorm":return this.builtins.Unpack2x16unorm(e,t);case"unpack2x16float":return this.builtins.Unpack2x16float(e,t);case"storageBarrier":return this.builtins.StorageBarrier(e,t);case"textureBarrier":return this.builtins.TextureBarrier(e,t);case"workgroupBarrier":return this.builtins.WorkgroupBarrier(e,t);case"workgroupUniformLoad":return this.builtins.WorkgroupUniformLoad(e,t);case"subgroupAdd":return this.builtins.SubgroupAdd(e,t);case"subgroupExclusiveAdd":return this.builtins.SubgroupExclusiveAdd(e,t);case"subgroupInclusiveAdd":return this.builtins.SubgroupInclusiveAdd(e,t);case"subgroupAll":return this.builtins.SubgroupAll(e,t);case"subgroupAnd":return this.builtins.SubgroupAnd(e,t);case"subgroupAny":return this.builtins.SubgroupAny(e,t);case"subgroupBallot":return this.builtins.SubgroupBallot(e,t);case"subgroupBroadcast":return this.builtins.SubgroupBroadcast(e,t);case"subgroupBroadcastFirst":return this.builtins.SubgroupBroadcastFirst(e,t);case"subgroupElect":return this.builtins.SubgroupElect(e,t);case"subgroupMax":return this.builtins.SubgroupMax(e,t);case"subgroupMin":return this.builtins.SubgroupMin(e,t);case"subgroupMul":return this.builtins.SubgroupMul(e,t);case"subgroupExclusiveMul":return this.builtins.SubgroupExclusiveMul(e,t);case"subgroupInclusiveMul":return this.builtins.SubgroupInclusiveMul(e,t);case"subgroupOr":return this.builtins.SubgroupOr(e,t);case"subgroupShuffle":return this.builtins.SubgroupShuffle(e,t);case"subgroupShuffleDown":return this.builtins.SubgroupShuffleDown(e,t);case"subgroupShuffleUp":return this.builtins.SubgroupShuffleUp(e,t);case"subgroupShuffleXor":return this.builtins.SubgroupShuffleXor(e,t);case"subgroupXor":return this.builtins.SubgroupXor(e,t);case"quadBroadcast":return this.builtins.QuadBroadcast(e,t);case"quadSwapDiagonal":return this.builtins.QuadSwapDiagonal(e,t);case"quadSwapX":return this.builtins.QuadSwapX(e,t);case"quadSwapY":return this.builtins.QuadSwapY(e,t)}let n=t.getFunction(e.name);if(n){let r=t.clone();for(let s=0;s<n.node.args.length;++s){let i=n.node.args[s],o=this.evalExpression(e.args[s],r);r.setVariable(i.name,o,i)}return this._execStatements(n.node.body,r)}return null}_callConstructorValue(e,t){if(!e.args||e.args.length===0)return new d(0,this.getTypeInfo(e.type));let n=this.evalExpression(e.args[0],t);return n.typeInfo=this.getTypeInfo(e.type),n.getSubData(this,e.postfix,t).clone()}_callConstructorVec(e,t){let n=this.getTypeInfo(e.type),r=e.type.getTypeName(),s=zt[r];if(s===void 0)return console.error(`Invalid vec constructor ${r}. Line ${e.line}`),null;let i=[];if(e instanceof R)if(e.isVector){let o=e.vectorValue;for(let l of o)i.push(l)}else i.push(e.scalarValue);else if(e.args)for(let o of e.args){let l=this.evalExpression(o,t);if(l instanceof p){let c=l.data;for(let u=0;u<c.length;++u){let h=c[u];i.push(h)}}else if(l instanceof d){let c=l.value;i.push(c)}}if(e.type instanceof g&&e.type.format===null&&(e.type.format=g.f32),i.length===0){let o=new Array(s).fill(0);return new p(o,n).getSubData(this,e.postfix,t)}if(i.length===1)for(;i.length<s;)i.push(i[0]);return i.length<s?(console.error(`Invalid vec constructor. Line ${e.line}`),null):new p(i.length>s?i.slice(0,s):i,n).getSubData(this,e.postfix,t)}_callConstructorMatrix(e,t){let n=this.getTypeInfo(e.type),r=e.type.getTypeName(),s=H[r];if(s===void 0)return console.error(`Invalid matrix constructor ${r}. Line ${e.line}`),null;let i=[];if(e instanceof R)if(e.isVector){let o=e.vectorValue;for(let l of o)i.push(l)}else i.push(e.scalarValue);else if(e.args)for(let o of e.args){let l=this.evalExpression(o,t);l instanceof p?i.push(...l.data):l instanceof d?i.push(l.value):l instanceof I&&i.push(...l.data)}if(n instanceof ie&&n.format===null&&(n.format=this.getTypeInfo("f32")),i.length===0){let o=new Array(s[2]).fill(0);return new I(o,n).getSubData(this,e.postfix,t)}return i.length!==s[2]?(console.error(`Invalid matrix constructor. Line ${e.line}`),null):new I(i,n).getSubData(this,e.postfix,t)}};V._breakObj=new q(new W("BREAK",null),null),V._continueObj=new q(new W("CONTINUE",null),null),V._priority=new Map([["f32",0],["f16",1],["u32",2],["i32",3],["x32",3]]);var ln=class{constructor(){this.constants=new Map,this.aliases=new Map,this.structs=new Map}},cn=class{constructor(){this._tokens=[],this._current=0,this._currentLine=1,this._deferArrayCountEval=[],this._currentLoop=[],this._context=new ln,this._exec=new V,this._forwardTypeCount=0}parse(e){this._initialize(e),this._deferArrayCountEval.length=0;let t=[];for(;!this._isAtEnd();){let n=this._global_decl_or_directive();if(!n)break;t.push(n)}if(this._deferArrayCountEval.length>0){for(let n of this._deferArrayCountEval){let r=n.arrayType,s=n.countNode;if(s instanceof G){let i=s.name,o=this._context.constants.get(i);if(o)try{let l=o.constEvaluate(this._exec);r.count=l}catch{}}}this._deferArrayCountEval.length=0}if(this._forwardTypeCount>0)for(let n of t)n.search(r=>{r instanceof At||r instanceof Re?r.type=this._forwardType(r.type):r instanceof _e?r.format=this._forwardType(r.format):r instanceof ee||r instanceof me||r instanceof ke?r.type=this._forwardType(r.type):r instanceof ge?r.returnType=this._forwardType(r.returnType):r instanceof St&&(r.type=this._forwardType(r.type))});return t}_forwardType(e){if(e instanceof vt){let t=this._getType(e.name);if(t)return t}else e instanceof Re?e.type=this._forwardType(e.type):e instanceof _e&&(e.format=this._forwardType(e.format));return e}_initialize(e){if(e)if(typeof e=="string"){let t=new rn(e);this._tokens=t.scanTokens()}else this._tokens=e;else this._tokens=[];this._current=0}_updateNode(e,t){return e.line=t??this._currentLine,e}_error(e,t){return{token:e,message:t,toString:()=>`${t}`}}_isAtEnd(){return this._current>=this._tokens.length||this._peek().type==f.eof}_match(e){if(e instanceof m)return!!this._check(e)&&(this._advance(),!0);for(let t=0,n=e.length;t<n;++t){let r=e[t];if(this._check(r))return this._advance(),!0}return!1}_consume(e,t){if(this._check(e))return this._advance();throw this._error(this._peek(),`${t}. Line:${this._currentLine}`)}_check(e){if(this._isAtEnd())return!1;let t=this._peek();if(e instanceof Array){let n=t.type,r=!1;for(let s of e){if(n===s)return!0;s===f.tokens.name&&(r=!0)}if(r){let s=f.tokens.name.rule.exec(t.lexeme);if(s&&s.index==0&&s[0]==t.lexeme)return!0}return!1}if(t.type===e)return!0;if(e===f.tokens.name){let n=f.tokens.name.rule.exec(t.lexeme);return n&&n.index==0&&n[0]==t.lexeme}return!1}_advance(){var e,t;return this._currentLine=(t=(e=this._peek())===null||e===void 0?void 0:e.line)!==null&&t!==void 0?t:-1,this._isAtEnd()||this._current++,this._previous()}_peek(){return this._tokens[this._current]}_previous(){return this._tokens[this._current-1]}_global_decl_or_directive(){for(;this._match(f.tokens.semicolon)&&!this._isAtEnd(););if(this._match(f.keywords.alias)){let t=this._type_alias();return this._consume(f.tokens.semicolon,"Expected ';'"),this._exec.reflection.updateAST([t]),t}if(this._match(f.keywords.diagnostic)){let t=this._diagnostic();return this._consume(f.tokens.semicolon,"Expected ';'"),this._exec.reflection.updateAST([t]),t}if(this._match(f.keywords.requires)){let t=this._requires_directive();return this._consume(f.tokens.semicolon,"Expected ';'"),this._exec.reflection.updateAST([t]),t}if(this._match(f.keywords.enable)){let t=this._enable_directive();return this._consume(f.tokens.semicolon,"Expected ';'"),this._exec.reflection.updateAST([t]),t}let e=this._attribute();if(this._check(f.keywords.var)){let t=this._global_variable_decl();return t!=null&&(t.attributes=e),this._consume(f.tokens.semicolon,"Expected ';'."),this._exec.reflection.updateAST([t]),t}if(this._check(f.keywords.override)){let t=this._override_variable_decl();return t!=null&&(t.attributes=e),this._consume(f.tokens.semicolon,"Expected ';'."),this._exec.reflection.updateAST([t]),t}if(this._check(f.keywords.let)){let t=this._global_let_decl();return t!=null&&(t.attributes=e),this._consume(f.tokens.semicolon,"Expected ';'."),this._exec.reflection.updateAST([t]),t}if(this._check(f.keywords.const)){let t=this._global_const_decl();return t!=null&&(t.attributes=e),this._consume(f.tokens.semicolon,"Expected ';'."),this._exec.reflection.updateAST([t]),t}if(this._check(f.keywords.struct)){let t=this._struct_decl();return t!=null&&(t.attributes=e),this._exec.reflection.updateAST([t]),t}if(this._check(f.keywords.fn)){let t=this._function_decl();return t!=null&&(t.attributes=e),this._exec.reflection.updateAST([t]),t}return null}_function_decl(){if(!this._match(f.keywords.fn))return null;let e=this._currentLine,t=this._consume(f.tokens.ident,"Expected function name.").toString();this._consume(f.tokens.paren_left,"Expected '(' for function arguments.");let n=[];if(!this._check(f.tokens.paren_right))do{if(this._check(f.tokens.paren_right))break;let o=this._attribute(),l=this._consume(f.tokens.name,"Expected argument name.").toString();this._consume(f.tokens.colon,"Expected ':' for argument type.");let c=this._attribute(),u=this._type_decl();u!=null&&(u.attributes=c,n.push(this._updateNode(new St(l,u,o))))}while(this._match(f.tokens.comma));this._consume(f.tokens.paren_right,"Expected ')' after function arguments.");let r=null;if(this._match(f.tokens.arrow)){let o=this._attribute();r=this._type_decl(),r!=null&&(r.attributes=o)}let s=this._compound_statement(),i=this._currentLine;return this._updateNode(new ge(t,n,r,s,e,i),e)}_compound_statement(){let e=[];for(this._consume(f.tokens.brace_left,"Expected '{' for block.");!this._check(f.tokens.brace_right);){let t=this._statement();t!==null&&e.push(t)}return this._consume(f.tokens.brace_right,"Expected '}' for block."),e}_statement(){for(;this._match(f.tokens.semicolon)&&!this._isAtEnd(););if(this._check(f.tokens.attr)&&this._attribute(),this._check(f.keywords.if))return this._if_statement();if(this._check(f.keywords.switch))return this._switch_statement();if(this._check(f.keywords.loop))return this._loop_statement();if(this._check(f.keywords.for))return this._for_statement();if(this._check(f.keywords.while))return this._while_statement();if(this._check(f.keywords.continuing))return this._continuing_statement();if(this._check(f.keywords.static_assert))return this._static_assert_statement();if(this._check(f.tokens.brace_left))return this._compound_statement();let e=null;if(this._check(f.keywords.return))e=this._return_statement();else if(this._check([f.keywords.var,f.keywords.let,f.keywords.const]))e=this._variable_statement();else if(this._match(f.keywords.discard))e=this._updateNode(new tn);else if(this._match(f.keywords.break)){let t=this._updateNode(new _t);if(this._currentLoop.length>0){let n=this._currentLoop[this._currentLoop.length-1];t.loopId=n.id}e=t,this._check(f.keywords.if)&&(this._advance(),t.condition=this._optional_paren_expression())}else if(this._match(f.keywords.continue)){let t=this._updateNode(new gt);if(!(this._currentLoop.length>0))throw this._error(this._peek(),`Continue statement must be inside a loop. Line: ${t.line}`);{let n=this._currentLoop[this._currentLoop.length-1];t.loopId=n.id}e=t}else e=this._increment_decrement_statement()||this._func_call_statement()||this._assignment_statement();return e!=null&&this._consume(f.tokens.semicolon,"Expected ';' after statement."),e}_static_assert_statement(){if(!this._match(f.keywords.static_assert))return null;let e=this._currentLine,t=this._optional_paren_expression();return this._updateNode(new Yt(t),e)}_while_statement(){if(!this._match(f.keywords.while))return null;let e=this._updateNode(new at(null,null));return this._currentLoop.push(e),e.condition=this._optional_paren_expression(),this._check(f.tokens.attr)&&this._attribute(),e.body=this._compound_statement(),this._currentLoop.pop(),e}_continuing_statement(){let e=this._currentLoop.length>0?this._currentLoop[this._currentLoop.length-1].id:-1;if(!this._match(f.keywords.continuing))return null;let t=this._currentLine,n=this._compound_statement();return this._updateNode(new Be(n,e),t)}_for_statement(){if(!this._match(f.keywords.for))return null;this._consume(f.tokens.paren_left,"Expected '('.");let e=this._updateNode(new lt(null,null,null,null));return this._currentLoop.push(e),e.init=this._check(f.tokens.semicolon)?null:this._for_init(),this._consume(f.tokens.semicolon,"Expected ';'."),e.condition=this._check(f.tokens.semicolon)?null:this._short_circuit_or_expression(),this._consume(f.tokens.semicolon,"Expected ';'."),e.increment=this._check(f.tokens.paren_right)?null:this._for_increment(),this._consume(f.tokens.paren_right,"Expected ')'."),this._check(f.tokens.attr)&&this._attribute(),e.body=this._compound_statement(),this._currentLoop.pop(),e}_for_init(){return this._variable_statement()||this._func_call_statement()||this._assignment_statement()}_for_increment(){return this._func_call_statement()||this._increment_decrement_statement()||this._assignment_statement()}_variable_statement(){if(this._check(f.keywords.var)){let e=this._variable_decl();if(e===null)throw this._error(this._peek(),"Variable declaration expected.");let t=null;return this._match(f.tokens.equal)&&(t=this._short_circuit_or_expression()),this._updateNode(new ee(e.name,e.type,e.storage,e.access,t),e.line)}if(this._match(f.keywords.let)){let e=this._currentLine,t=this._consume(f.tokens.name,"Expected name for let.").toString(),n=null;if(this._match(f.tokens.colon)){let s=this._attribute();n=this._type_decl(),n!=null&&(n.attributes=s)}this._consume(f.tokens.equal,"Expected '=' for let.");let r=this._short_circuit_or_expression();return this._updateNode(new me(t,n,null,null,r),e)}if(this._match(f.keywords.const)){let e=this._currentLine,t=this._consume(f.tokens.name,"Expected name for const.").toString(),n=null;if(this._match(f.tokens.colon)){let s=this._attribute();n=this._type_decl(),n!=null&&(n.attributes=s)}this._consume(f.tokens.equal,"Expected '=' for const.");let r=this._short_circuit_or_expression();return n===null&&r instanceof R&&(n=r.type),this._updateNode(new ke(t,n,null,null,r),e)}return null}_increment_decrement_statement(){let e=this._current,t=this._unary_expression();if(t==null)return null;if(!this._check(f.increment_operators))return this._current=e,null;let n=this._consume(f.increment_operators,"Expected increment operator");return this._updateNode(new ct(n.type===f.tokens.plus_plus?ye.increment:ye.decrement,t))}_assignment_statement(){let e=null,t=this._currentLine;if(this._check(f.tokens.brace_right))return null;let n=this._match(f.tokens.underscore);if(n||(e=this._unary_expression()),!n&&e==null)return null;let r=this._consume(f.assignment_operators,"Expected assignment operator."),s=this._short_circuit_or_expression();return this._updateNode(new ut(Me.parse(r.lexeme),e,s),t)}_func_call_statement(){if(!this._check(f.tokens.ident))return null;let e=this._currentLine,t=this._current,n=this._consume(f.tokens.ident,"Expected function name."),r=this._argument_expression_list();return r===null?(this._current=t,null):this._updateNode(new Ue(n.lexeme,r),e)}_loop_statement(){if(!this._match(f.keywords.loop))return null;this._check(f.tokens.attr)&&this._attribute(),this._consume(f.tokens.brace_left,"Expected '{' for loop.");let e=this._updateNode(new ft([],null));this._currentLoop.push(e);let t=this._statement();for(;t!==null;){if(Array.isArray(t))for(let n of t)e.body.push(n);else e.body.push(t);if(t instanceof Be){e.continuing=t;break}t=this._statement()}return this._currentLoop.pop(),this._consume(f.tokens.brace_right,"Expected '}' for loop."),e}_switch_statement(){if(!this._match(f.keywords.switch))return null;let e=this._updateNode(new ht(null,[]));if(this._currentLoop.push(e),e.condition=this._optional_paren_expression(),this._check(f.tokens.attr)&&this._attribute(),this._consume(f.tokens.brace_left,"Expected '{' for switch."),e.cases=this._switch_body(),e.cases==null||e.cases.length==0)throw this._error(this._previous(),"Expected 'case' or 'default'.");return this._consume(f.tokens.brace_right,"Expected '}' for switch."),this._currentLoop.pop(),e}_switch_body(){let e=[],t=!1;for(;this._check([f.keywords.default,f.keywords.case]);){if(this._match(f.keywords.case)){let n=this._case_selectors();for(let s of n)if(s instanceof Ie){if(t)throw this._error(this._previous(),"Multiple default cases in switch statement.");t=!0;break}this._match(f.tokens.colon),this._check(f.tokens.attr)&&this._attribute(),this._consume(f.tokens.brace_left,"Exected '{' for switch case.");let r=this._case_body();this._consume(f.tokens.brace_right,"Exected '}' for switch case."),e.push(this._updateNode(new kt(n,r)))}if(this._match(f.keywords.default)){if(t)throw this._error(this._previous(),"Multiple default cases in switch statement.");this._match(f.tokens.colon),this._check(f.tokens.attr)&&this._attribute(),this._consume(f.tokens.brace_left,"Exected '{' for switch default.");let n=this._case_body();this._consume(f.tokens.brace_right,"Exected '}' for switch default."),e.push(this._updateNode(new It(n)))}}return e}_case_selectors(){let e=[];for(this._match(f.keywords.default)?e.push(this._updateNode(new Ie)):e.push(this._shift_expression());this._match(f.tokens.comma);)this._match(f.keywords.default)?e.push(this._updateNode(new Ie)):e.push(this._shift_expression());return e}_case_body(){if(this._match(f.keywords.fallthrough))return this._consume(f.tokens.semicolon,"Expected ';'"),[];let e=this._statement();if(e==null)return[];e instanceof Array||(e=[e]);let t=this._case_body();return t.length==0?e:[...e,t[0]]}_if_statement(){if(!this._match(f.keywords.if))return null;let e=this._currentLine,t=this._optional_paren_expression();this._check(f.tokens.attr)&&this._attribute();let n=this._compound_statement(),r=[];this._match_elseif()&&(this._check(f.tokens.attr)&&this._attribute(),r=this._elseif_statement(r));let s=null;return this._match(f.keywords.else)&&(this._check(f.tokens.attr)&&this._attribute(),s=this._compound_statement()),this._updateNode(new pt(t,n,r,s),e)}_match_elseif(){return this._tokens[this._current].type===f.keywords.else&&this._tokens[this._current+1].type===f.keywords.if&&(this._advance(),this._advance(),!0)}_elseif_statement(e=[]){let t=this._optional_paren_expression(),n=this._compound_statement();return e.push(this._updateNode(new nn(t,n))),this._match_elseif()&&(this._check(f.tokens.attr)&&this._attribute(),this._elseif_statement(e)),e}_return_statement(){if(!this._match(f.keywords.return))return null;let e=this._short_circuit_or_expression();return this._updateNode(new dt(e))}_short_circuit_or_expression(){let e=this._short_circuit_and_expr();for(;this._match(f.tokens.or_or);)e=this._updateNode(new j(this._previous().toString(),e,this._short_circuit_and_expr()));return e}_short_circuit_and_expr(){let e=this._inclusive_or_expression();for(;this._match(f.tokens.and_and);)e=this._updateNode(new j(this._previous().toString(),e,this._inclusive_or_expression()));return e}_inclusive_or_expression(){let e=this._exclusive_or_expression();for(;this._match(f.tokens.or);)e=this._updateNode(new j(this._previous().toString(),e,this._exclusive_or_expression()));return e}_exclusive_or_expression(){let e=this._and_expression();for(;this._match(f.tokens.xor);)e=this._updateNode(new j(this._previous().toString(),e,this._and_expression()));return e}_and_expression(){let e=this._equality_expression();for(;this._match(f.tokens.and);)e=this._updateNode(new j(this._previous().toString(),e,this._equality_expression()));return e}_equality_expression(){let e=this._relational_expression();return this._match([f.tokens.equal_equal,f.tokens.not_equal])?this._updateNode(new j(this._previous().toString(),e,this._relational_expression())):e}_relational_expression(){let e=this._shift_expression();for(;this._match([f.tokens.less_than,f.tokens.greater_than,f.tokens.less_than_equal,f.tokens.greater_than_equal]);)e=this._updateNode(new j(this._previous().toString(),e,this._shift_expression()));return e}_shift_expression(){let e=this._additive_expression();for(;this._match([f.tokens.shift_left,f.tokens.shift_right]);)e=this._updateNode(new j(this._previous().toString(),e,this._additive_expression()));return e}_additive_expression(){let e=this._multiplicative_expression();for(;this._match([f.tokens.plus,f.tokens.minus]);)e=this._updateNode(new j(this._previous().toString(),e,this._multiplicative_expression()));return e}_multiplicative_expression(){let e=this._unary_expression();for(;this._match([f.tokens.star,f.tokens.forward_slash,f.tokens.modulo]);)e=this._updateNode(new j(this._previous().toString(),e,this._unary_expression()));return e}_unary_expression(){return this._match([f.tokens.minus,f.tokens.bang,f.tokens.tilde,f.tokens.star,f.tokens.and])?this._updateNode(new M(this._previous().toString(),this._unary_expression())):this._singular_expression()}_singular_expression(){let e=this._primary_expression(),t=this._postfix_expression();return t&&(e.postfix=t),e}_postfix_expression(){if(this._match(f.tokens.bracket_left)){let e=this._short_circuit_or_expression();this._consume(f.tokens.bracket_right,"Expected ']'.");let t=this._updateNode(new fe(e)),n=this._postfix_expression();return n&&(t.postfix=n),t}if(this._match(f.tokens.period)){let e=this._consume(f.tokens.name,"Expected member name."),t=this._postfix_expression(),n=this._updateNode(new ae(e.lexeme));return t&&(n.postfix=t),n}return null}_getStruct(e){return this._context.aliases.has(e)?this._context.aliases.get(e).type:this._context.structs.has(e)?this._context.structs.get(e):null}_getType(e){let t=this._getStruct(e);if(t!==null)return t;switch(e){case"void":return x.void;case"bool":return x.bool;case"i32":return x.i32;case"u32":return x.u32;case"f32":return x.f32;case"f16":return x.f16;case"vec2f":return g.vec2f;case"vec3f":return g.vec3f;case"vec4f":return g.vec4f;case"vec2i":return g.vec2i;case"vec3i":return g.vec3i;case"vec4i":return g.vec4i;case"vec2u":return g.vec2u;case"vec3u":return g.vec3u;case"vec4u":return g.vec4u;case"vec2h":return g.vec2h;case"vec3h":return g.vec3h;case"vec4h":return g.vec4h;case"mat2x2f":return g.mat2x2f;case"mat2x3f":return g.mat2x3f;case"mat2x4f":return g.mat2x4f;case"mat3x2f":return g.mat3x2f;case"mat3x3f":return g.mat3x3f;case"mat3x4f":return g.mat3x4f;case"mat4x2f":return g.mat4x2f;case"mat4x3f":return g.mat4x3f;case"mat4x4f":return g.mat4x4f;case"mat2x2h":return g.mat2x2h;case"mat2x3h":return g.mat2x3h;case"mat2x4h":return g.mat2x4h;case"mat3x2h":return g.mat3x2h;case"mat3x3h":return g.mat3x3h;case"mat3x4h":return g.mat3x4h;case"mat4x2h":return g.mat4x2h;case"mat4x3h":return g.mat4x3h;case"mat4x4h":return g.mat4x4h;case"mat2x2i":return g.mat2x2i;case"mat2x3i":return g.mat2x3i;case"mat2x4i":return g.mat2x4i;case"mat3x2i":return g.mat3x2i;case"mat3x3i":return g.mat3x3i;case"mat3x4i":return g.mat3x4i;case"mat4x2i":return g.mat4x2i;case"mat4x3i":return g.mat4x3i;case"mat4x4i":return g.mat4x4i;case"mat2x2u":return g.mat2x2u;case"mat2x3u":return g.mat2x3u;case"mat2x4u":return g.mat2x4u;case"mat3x2u":return g.mat3x2u;case"mat3x3u":return g.mat3x3u;case"mat3x4u":return g.mat3x4u;case"mat4x2u":return g.mat4x2u;case"mat4x3u":return g.mat4x3u;case"mat4x4u":return g.mat4x4u}return null}_validateTypeRange(e,t){if(t.name==="i32"){if(e<-2147483648||e>2147483647)throw this._error(this._previous(),`Value out of range for i32: ${e}. Line: ${this._currentLine}.`)}else if(t.name==="u32"&&(e<0||e>4294967295))throw this._error(this._previous(),`Value out of range for u32: ${e}. Line: ${this._currentLine}.`)}_primary_expression(){if(this._match(f.tokens.ident)){let n=this._previous().toString();if(this._check(f.tokens.paren_left)){let r=this._argument_expression_list(),s=this._getType(n);return s!==null?this._updateNode(new K(s,r)):this._updateNode(new He(n,r))}if(this._context.constants.has(n)){let r=this._context.constants.get(n);return this._updateNode(new bt(n,r.value))}return this._updateNode(new G(n))}if(this._match(f.tokens.int_literal)){let n=this._previous().toString(),r=n.endsWith("i")||n.endsWith("i")?x.i32:n.endsWith("u")||n.endsWith("U")?x.u32:x.x32,s=parseInt(n);return this._validateTypeRange(s,r),this._updateNode(new R(new d(s,this._exec.getTypeInfo(r)),r))}if(this._match(f.tokens.uint_literal)){let n=parseInt(this._previous().toString());return this._validateTypeRange(n,x.u32),this._updateNode(new R(new d(n,this._exec.getTypeInfo(x.u32)),x.u32))}if(this._match([f.tokens.decimal_float_literal,f.tokens.hex_float_literal])){let n=this._previous().toString(),r=n.endsWith("h");r&&(n=n.substring(0,n.length-1));let s=parseFloat(n);this._validateTypeRange(s,r?x.f16:x.f32);let i=r?x.f16:x.f32;return this._updateNode(new R(new d(s,this._exec.getTypeInfo(i)),i))}if(this._match([f.keywords.true,f.keywords.false])){let n=this._previous().toString()===f.keywords.true.rule;return this._updateNode(new R(new d(n?1:0,this._exec.getTypeInfo(x.bool)),x.bool))}if(this._check(f.tokens.paren_left))return this._paren_expression();if(this._match(f.keywords.bitcast)){this._consume(f.tokens.less_than,"Expected '<'.");let n=this._type_decl();this._consume(f.tokens.greater_than,"Expected '>'.");let r=this._paren_expression();return this._updateNode(new xt(n,r))}let e=this._type_decl(),t=this._argument_expression_list();return this._updateNode(new K(e,t))}_argument_expression_list(){if(!this._match(f.tokens.paren_left))return null;let e=[];do{if(this._check(f.tokens.paren_right))break;let t=this._short_circuit_or_expression();e.push(t)}while(this._match(f.tokens.comma));return this._consume(f.tokens.paren_right,"Expected ')' for agument list"),e}_optional_paren_expression(){this._match(f.tokens.paren_left);let e=this._short_circuit_or_expression();return this._match(f.tokens.paren_right),e}_paren_expression(){this._consume(f.tokens.paren_left,"Expected '('.");let e=this._short_circuit_or_expression();return this._consume(f.tokens.paren_right,"Expected ')'."),e}_struct_decl(){if(!this._match(f.keywords.struct))return null;let e=this._currentLine,t=this._consume(f.tokens.ident,"Expected name for struct.").toString();this._consume(f.tokens.brace_left,"Expected '{' for struct body.");let n=[];for(;!this._check(f.tokens.brace_right);){let i=this._attribute(),o=this._consume(f.tokens.name,"Expected variable name.").toString();this._consume(f.tokens.colon,"Expected ':' for struct member type.");let l=this._attribute(),c=this._type_decl();c!=null&&(c.attributes=l),this._check(f.tokens.brace_right)?this._match(f.tokens.comma):this._consume(f.tokens.comma,"Expected ',' for struct member."),n.push(this._updateNode(new At(o,c,i)))}this._consume(f.tokens.brace_right,"Expected '}' after struct body.");let r=this._currentLine,s=this._updateNode(new Y(t,n,e,r),e);return this._context.structs.set(t,s),s}_global_variable_decl(){let e=this._variable_decl();if(!e)return null;if(this._match(f.tokens.equal)){let t=this._const_expression();e.value=t}if(e.type!==null&&e.value instanceof R){if(e.value.type.name!=="x32"&&e.type.getTypeName()!==e.value.type.getTypeName())throw this._error(this._peek(),`Invalid cast from ${e.value.type.name} to ${e.type.name}. Line:${this._currentLine}`);e.value.isScalar&&this._validateTypeRange(e.value.scalarValue,e.type),e.value.type=e.type}else e.type===null&&e.value instanceof R&&(e.type=e.value.type.name==="x32"?x.i32:e.value.type,e.value.isScalar&&this._validateTypeRange(e.value.scalarValue,e.type));return e}_override_variable_decl(){let e=this._override_decl();return e&&this._match(f.tokens.equal)&&(e.value=this._const_expression()),e}_global_const_decl(){var e;if(!this._match(f.keywords.const))return null;let t=this._consume(f.tokens.name,"Expected variable name"),n=this._currentLine,r=null;if(this._match(f.tokens.colon)){let l=this._attribute();r=this._type_decl(),r!=null&&(r.attributes=l)}let s=null;this._consume(f.tokens.equal,"const declarations require an assignment");let i=this._short_circuit_or_expression();try{let l=[x.f32],c=i.constEvaluate(this._exec,l);c instanceof d&&this._validateTypeRange(c.value,l[0]),l[0]instanceof g&&l[0].format===null&&c.typeInfo instanceof ie&&c.typeInfo.format!==null&&(c.typeInfo.format.name==="f16"?l[0].format=x.f16:c.typeInfo.format.name==="f32"?l[0].format=x.f32:c.typeInfo.format.name==="i32"?l[0].format=x.i32:c.typeInfo.format.name==="u32"?l[0].format=x.u32:c.typeInfo.format.name==="bool"?l[0].format=x.bool:console.error(`TODO: impelement template format type ${c.typeInfo.format.name}`)),s=this._updateNode(new R(c,l[0])),this._exec.context.setVariable(t.toString(),c)}catch{s=i}if(r!==null&&s instanceof R){if(s.type.name!=="x32"&&r.getTypeName()!==s.type.getTypeName())throw this._error(this._peek(),`Invalid cast from ${s.type.name} to ${r.name}. Line:${this._currentLine}`);s.type=r,s.isScalar&&this._validateTypeRange(s.scalarValue,s.type)}else r===null&&s instanceof R&&(r=(e=s?.type)!==null&&e!==void 0?e:x.f32,r===x.x32&&(r=x.i32));let o=this._updateNode(new ke(t.toString(),r,"","",s),n);return this._context.constants.set(o.name,o),o}_global_let_decl(){if(!this._match(f.keywords.let))return null;let e=this._currentLine,t=this._consume(f.tokens.name,"Expected variable name"),n=null;if(this._match(f.tokens.colon)){let s=this._attribute();n=this._type_decl(),n!=null&&(n.attributes=s)}let r=null;if(this._match(f.tokens.equal)&&(r=this._const_expression()),n!==null&&r instanceof R){if(r.type.name!=="x32"&&n.getTypeName()!==r.type.getTypeName())throw this._error(this._peek(),`Invalid cast from ${r.type.name} to ${n.name}. Line:${this._currentLine}`);r.type=n}else n===null&&r instanceof R&&(n=r.type.name==="x32"?x.i32:r.type);return r instanceof R&&r.isScalar&&this._validateTypeRange(r.scalarValue,n),this._updateNode(new me(t.toString(),n,"","",r),e)}_const_expression(){return this._short_circuit_or_expression()}_variable_decl(){if(!this._match(f.keywords.var))return null;let e=this._currentLine,t="",n="";this._match(f.tokens.less_than)&&(t=this._consume(f.storage_class,"Expected storage_class.").toString(),this._match(f.tokens.comma)&&(n=this._consume(f.access_mode,"Expected access_mode.").toString()),this._consume(f.tokens.greater_than,"Expected '>'."));let r=this._consume(f.tokens.name,"Expected variable name"),s=null;if(this._match(f.tokens.colon)){let i=this._attribute();s=this._type_decl(),s!=null&&(s.attributes=i)}return this._updateNode(new ee(r.toString(),s,t,n,null),e)}_override_decl(){if(!this._match(f.keywords.override))return null;let e=this._consume(f.tokens.name,"Expected variable name"),t=null;if(this._match(f.tokens.colon)){let n=this._attribute();t=this._type_decl(),t!=null&&(t.attributes=n)}return this._updateNode(new Ve(e.toString(),t,null))}_diagnostic(){this._consume(f.tokens.paren_left,"Expected '('");let e=this._consume(f.tokens.ident,"Expected severity control name.");this._consume(f.tokens.comma,"Expected ','");let t=this._consume(f.tokens.ident,"Expected diagnostic rule name.").toString();return this._match(f.tokens.period)&&(t+=`.${this._consume(f.tokens.ident,"Expected diagnostic message.").toString()}`),this._consume(f.tokens.paren_right,"Expected ')'"),this._updateNode(new mt(e.toString(),t))}_enable_directive(){let e=this._consume(f.tokens.ident,"identity expected.");return this._updateNode(new Jt(e.toString()))}_requires_directive(){let e=[this._consume(f.tokens.ident,"identity expected.").toString()];for(;this._match(f.tokens.comma);){let t=this._consume(f.tokens.ident,"identity expected.");e.push(t.toString())}return this._updateNode(new en(e))}_type_alias(){let e=this._consume(f.tokens.ident,"identity expected.");this._consume(f.tokens.equal,"Expected '=' for type alias.");let t=this._type_decl();if(t===null)throw this._error(this._peek(),"Expected Type for Alias.");this._context.aliases.has(t.name)&&(t=this._context.aliases.get(t.name).type);let n=this._updateNode(new $e(e.toString(),t));return this._context.aliases.set(n.name,n),n}_type_decl(){if(this._check([f.tokens.ident,...f.texel_format,f.keywords.bool,f.keywords.f32,f.keywords.i32,f.keywords.u32])){let n=this._advance().toString();if(this._context.structs.has(n))return this._context.structs.get(n);if(this._context.aliases.has(n))return this._context.aliases.get(n).type;if(!this._getType(n)){let r=this._updateNode(new vt(n));return this._forwardTypeCount++,r}return this._updateNode(new x(n))}let e=this._texture_sampler_types();if(e)return e;if(this._check(f.template_types)){let n=this._advance().toString(),r=null,s=null;return this._match(f.tokens.less_than)&&(r=this._type_decl(),s=null,this._match(f.tokens.comma)&&(s=this._consume(f.access_mode,"Expected access_mode for pointer").toString()),this._consume(f.tokens.greater_than,"Expected '>' for type.")),this._updateNode(new g(n,r,s))}if(this._match(f.keywords.ptr)){let n=this._previous().toString();this._consume(f.tokens.less_than,"Expected '<' for pointer.");let r=this._consume(f.storage_class,"Expected storage_class for pointer");this._consume(f.tokens.comma,"Expected ',' for pointer.");let s=this._type_decl(),i=null;return this._match(f.tokens.comma)&&(i=this._consume(f.access_mode,"Expected access_mode for pointer").toString()),this._consume(f.tokens.greater_than,"Expected '>' for pointer."),this._updateNode(new Re(n,r.toString(),s,i))}let t=this._attribute();if(this._match(f.keywords.array)){let n=null,r=-1,s=this._previous(),i=null;if(this._match(f.tokens.less_than)){n=this._type_decl(),this._context.aliases.has(n.name)&&(n=this._context.aliases.get(n.name).type);let l="";if(this._match(f.tokens.comma)){i=this._shift_expression();try{l=i.constEvaluate(this._exec).toString(),i=null}catch{l="1"}}this._consume(f.tokens.greater_than,"Expected '>' for array."),r=l?parseInt(l):0}let o=this._updateNode(new _e(s.toString(),t,n,r));return i&&this._deferArrayCountEval.push({arrayType:o,countNode:i}),o}return null}_texture_sampler_types(){if(this._match(f.sampler_type))return this._updateNode(new de(this._previous().toString(),null,null));if(this._match(f.depth_texture_type))return this._updateNode(new de(this._previous().toString(),null,null));if(this._match(f.sampled_texture_type)||this._match(f.multisampled_texture_type)){let e=this._previous();this._consume(f.tokens.less_than,"Expected '<' for sampler type.");let t=this._type_decl();return this._consume(f.tokens.greater_than,"Expected '>' for sampler type."),this._updateNode(new de(e.toString(),t,null))}if(this._match(f.storage_texture_type)){let e=this._previous();this._consume(f.tokens.less_than,"Expected '<' for sampler type.");let t=this._consume(f.texel_format,"Invalid texel format.").toString();this._consume(f.tokens.comma,"Expected ',' after texel format.");let n=this._consume(f.access_mode,"Expected access mode for storage texture type.").toString();return this._consume(f.tokens.greater_than,"Expected '>' for sampler type."),this._updateNode(new de(e.toString(),t,n))}return null}_attribute(){let e=[];for(;this._match(f.tokens.attr);){let t=this._consume(f.attribute_name,"Expected attribute name"),n=this._updateNode(new Tt(t.toString(),null));if(this._match(f.tokens.paren_left)){if(n.value=this._consume(f.literal_or_ident,"Expected attribute value").toString(),this._check(f.tokens.comma)){this._advance();do{let r=this._consume(f.literal_or_ident,"Expected attribute value").toString();n.value instanceof Array||(n.value=[n.value]),n.value.push(r)}while(this._match(f.tokens.comma))}this._consume(f.tokens.paren_right,"Expected ')'")}e.push(n)}return e.length==0?null:e}},Ot=class extends Z{constructor(e){super(),e&&this.update(e)}update(e){let t=new cn().parse(e);this.updateAST(t)}};function Jn(a){let e={attributes:[],bindings:[]},t;try{t=is(a)}catch(s){return Yn.log.error(s.message)(),e}for(let s of t.uniforms){let i=[];for(let o of s.type?.members||[])i.push({name:o.name,type:Qn(o.type)});e.bindings.push({type:"uniform",name:s.name,group:s.group,location:s.binding,members:i})}for(let s of t.textures)e.bindings.push({type:"texture",name:s.name,group:s.group,location:s.binding});for(let s of t.samplers)e.bindings.push({type:"sampler",name:s.name,group:s.group,location:s.binding});let n=t.entry.vertex[0],r=n?.inputs.length||0;for(let s=0;s<r;s++){let i=n.inputs[s];if(i.locationType==="location"){let o=Qn(i.type);e.attributes.push({name:i.name,location:Number(i.location),type:o})}}return e}function Qn(a){return a?.format?`${a.name}<${a.format.name}>`:a.name}function is(a){try{return new Ot(a)}catch(e){if(e instanceof Error)throw e;let t="WGSL parse error";throw typeof e=="object"&&e?.message&&(t+=`: ${e.message} `),typeof e=="object"&&e?.token&&(t+=e.token.line||""),new Error(t,{cause:e})}}var xo=1/Math.PI*180,yo=1/180*Math.PI,as={EPSILON:1e-12,debug:!1,precision:4,printTypes:!1,printDegrees:!1,printRowMajor:!0,_cartographicRadians:!1};globalThis.mathgl=globalThis.mathgl||{config:{...as}};var ls=globalThis.mathgl.config;function er(a){return Array.isArray(a)||ArrayBuffer.isView(a)&&!(a instanceof DataView)}function fn(a,e,t){return us(a,n=>Math.max(e,Math.min(t,n)))}function cs(a){return a.clone?a.clone():new Array(a.length)}function us(a,e,t){if(er(a)){let n=a;t=t||cs(n);for(let r=0;r<t.length&&r<n.length;++r){let s=typeof a=="number"?a:a[r];t[r]=e(s,r,t)}return t}return e(a)}var ve=null,tr=new ArrayBuffer(4),nr=new Float32Array(tr),rr=new Uint32Array(tr);function sr(a){ve||=ir(),a=fn(a,-65504,65504),nr[0]=a;let e=rr[0],t=e>>23&511;return ve.baseTable[t]+((e&8388607)>>ve.shiftTable[t])}function or(a){ve||=ir();let e=a>>10;return rr[0]=ve.mantissaTable[ve.offsetTable[e]+(a&1023)]+ve.exponentTable[e],nr[0]}function ir(){let a=new Uint32Array(512),e=new Uint32Array(512);for(let s=0;s<256;++s){let i=s-127;i<-27?(a[s]=0,a[s|256]=32768,e[s]=24,e[s|256]=24):i<-14?(a[s]=1024>>-i-14,a[s|256]=1024>>-i-14|32768,e[s]=-i-1,e[s|256]=-i-1):i<=15?(a[s]=i+15<<10,a[s|256]=i+15<<10|32768,e[s]=13,e[s|256]=13):i<128?(a[s]=31744,a[s|256]=64512,e[s]=24,e[s|256]=24):(a[s]=31744,a[s|256]=64512,e[s]=13,e[s|256]=13)}let t=new Uint32Array(2048),n=new Uint32Array(64),r=new Uint32Array(64);for(let s=1;s<1024;++s){let i=s<<13,o=0;for(;!(i&8388608);)i<<=1,o-=8388608;i&=-8388609,o+=947912704,t[s]=i|o}for(let s=1024;s<2048;++s)t[s]=939524096+(s-1024<<13);for(let s=1;s<31;++s)n[s]=s<<23;n[31]=1199570944,n[32]=2147483648;for(let s=33;s<63;++s)n[s]=2147483648+(s-32<<23);n[63]=3347054592;for(let s=1;s<64;++s)s!==32&&(r[s]=1024);return{baseTable:a,shiftTable:e,mantissaTable:t,exponentTable:n,offsetTable:r}}function Te(a,e=[],t=0){let n=Math.fround(a),r=a-n;return e[t]=n,e[t+1]=r,e}function je(a){return a-Math.fround(a)}function Xe(a){let e=new Float32Array(32);for(let t=0;t<4;++t)for(let n=0;n<4;++n){let r=t*4+n;Te(a[n*4+t],e,r*2)}return e}var fs=`fn random(scale: vec3f, seed: float) -> f32 {
  /* use the fragment position for a different seed per-pixel */
  return fract(sin(dot(gl_FragCoord.xyz + seed, scale)) * 43758.5453 + seed);
}
`,hs=`float random(vec3 scale, float seed) {
  /* use the fragment position for a different seed per-pixel */
  return fract(sin(dot(gl_FragCoord.xyz + seed, scale)) * 43758.5453 + seed);
}
`,ar={name:"random",source:fs,fs:hs};var ps=`#ifdef LUMA_FP32_TAN_PRECISION_WORKAROUND

// All these functions are for substituting tan() function from Intel GPU only
const float TWO_PI = 6.2831854820251465;
const float PI_2 = 1.5707963705062866;
const float PI_16 = 0.1963495463132858;

const float SIN_TABLE_0 = 0.19509032368659973;
const float SIN_TABLE_1 = 0.3826834261417389;
const float SIN_TABLE_2 = 0.5555702447891235;
const float SIN_TABLE_3 = 0.7071067690849304;

const float COS_TABLE_0 = 0.9807852506637573;
const float COS_TABLE_1 = 0.9238795042037964;
const float COS_TABLE_2 = 0.8314695954322815;
const float COS_TABLE_3 = 0.7071067690849304;

const float INVERSE_FACTORIAL_3 = 1.666666716337204e-01; // 1/3!
const float INVERSE_FACTORIAL_5 = 8.333333767950535e-03; // 1/5!
const float INVERSE_FACTORIAL_7 = 1.9841270113829523e-04; // 1/7!
const float INVERSE_FACTORIAL_9 = 2.75573188446287533e-06; // 1/9!

float sin_taylor_fp32(float a) {
  float r, s, t, x;

  if (a == 0.0) {
    return 0.0;
  }

  x = -a * a;
  s = a;
  r = a;

  r = r * x;
  t = r * INVERSE_FACTORIAL_3;
  s = s + t;

  r = r * x;
  t = r * INVERSE_FACTORIAL_5;
  s = s + t;

  r = r * x;
  t = r * INVERSE_FACTORIAL_7;
  s = s + t;

  r = r * x;
  t = r * INVERSE_FACTORIAL_9;
  s = s + t;

  return s;
}

void sincos_taylor_fp32(float a, out float sin_t, out float cos_t) {
  if (a == 0.0) {
    sin_t = 0.0;
    cos_t = 1.0;
  }
  sin_t = sin_taylor_fp32(a);
  cos_t = sqrt(1.0 - sin_t * sin_t);
}

float tan_taylor_fp32(float a) {
    float sin_a;
    float cos_a;

    if (a == 0.0) {
        return 0.0;
    }

    // 2pi range reduction
    float z = floor(a / TWO_PI);
    float r = a - TWO_PI * z;

    float t;
    float q = floor(r / PI_2 + 0.5);
    int j = int(q);

    if (j < -2 || j > 2) {
        return 1.0 / 0.0;
    }

    t = r - PI_2 * q;

    q = floor(t / PI_16 + 0.5);
    int k = int(q);
    int abs_k = int(abs(float(k)));

    if (abs_k > 4) {
        return 1.0 / 0.0;
    } else {
        t = t - PI_16 * q;
    }

    float u = 0.0;
    float v = 0.0;

    float sin_t, cos_t;
    float s, c;
    sincos_taylor_fp32(t, sin_t, cos_t);

    if (k == 0) {
        s = sin_t;
        c = cos_t;
    } else {
        if (abs(float(abs_k) - 1.0) < 0.5) {
            u = COS_TABLE_0;
            v = SIN_TABLE_0;
        } else if (abs(float(abs_k) - 2.0) < 0.5) {
            u = COS_TABLE_1;
            v = SIN_TABLE_1;
        } else if (abs(float(abs_k) - 3.0) < 0.5) {
            u = COS_TABLE_2;
            v = SIN_TABLE_2;
        } else if (abs(float(abs_k) - 4.0) < 0.5) {
            u = COS_TABLE_3;
            v = SIN_TABLE_3;
        }
        if (k > 0) {
            s = u * sin_t + v * cos_t;
            c = u * cos_t - v * sin_t;
        } else {
            s = u * sin_t - v * cos_t;
            c = u * cos_t + v * sin_t;
        }
    }

    if (j == 0) {
        sin_a = s;
        cos_a = c;
    } else if (j == 1) {
        sin_a = c;
        cos_a = -s;
    } else if (j == -1) {
        sin_a = -c;
        cos_a = s;
    } else {
        sin_a = -s;
        cos_a = -c;
    }
    return sin_a / cos_a;
}
#endif

float tan_fp32(float a) {
#ifdef LUMA_FP32_TAN_PRECISION_WORKAROUND
  return tan_taylor_fp32(a);
#else
  return tan(a);
#endif
}
`,lr={name:"fp32",vs:ps};var cr=`
uniform fp64arithmeticUniforms {
  uniform float ONE;
} fp64;

/*
About LUMA_FP64_CODE_ELIMINATION_WORKAROUND

The purpose of this workaround is to prevent shader compilers from
optimizing away necessary arithmetic operations by swapping their sequences
or transform the equation to some 'equivalent' form.

The method is to multiply an artifical variable, ONE, which will be known to
the compiler to be 1 only at runtime. The whole expression is then represented
as a polynomial with respective to ONE. In the coefficients of all terms, only one a
and one b should appear

err = (a + b) * ONE^6 - a * ONE^5 - (a + b) * ONE^4 + a * ONE^3 - b - (a + b) * ONE^2 + a * ONE
*/

// Divide float number to high and low floats to extend fraction bits
vec2 split(float a) {
  const float SPLIT = 4097.0;
  float t = a * SPLIT;
#if defined(LUMA_FP64_CODE_ELIMINATION_WORKAROUND)
  float a_hi = t * fp64.ONE - (t - a);
  float a_lo = a * fp64.ONE - a_hi;
#else
  float a_hi = t - (t - a);
  float a_lo = a - a_hi;
#endif
  return vec2(a_hi, a_lo);
}

// Divide float number again when high float uses too many fraction bits
vec2 split2(vec2 a) {
  vec2 b = split(a.x);
  b.y += a.y;
  return b;
}

// Special sum operation when a > b
vec2 quickTwoSum(float a, float b) {
#if defined(LUMA_FP64_CODE_ELIMINATION_WORKAROUND)
  float sum = (a + b) * fp64.ONE;
  float err = b - (sum - a) * fp64.ONE;
#else
  float sum = a + b;
  float err = b - (sum - a);
#endif
  return vec2(sum, err);
}

// General sum operation
vec2 twoSum(float a, float b) {
  float s = (a + b);
#if defined(LUMA_FP64_CODE_ELIMINATION_WORKAROUND)
  float v = (s * fp64.ONE - a) * fp64.ONE;
  float err = (a - (s - v) * fp64.ONE) * fp64.ONE * fp64.ONE * fp64.ONE + (b - v);
#else
  float v = s - a;
  float err = (a - (s - v)) + (b - v);
#endif
  return vec2(s, err);
}

vec2 twoSub(float a, float b) {
  float s = (a - b);
#if defined(LUMA_FP64_CODE_ELIMINATION_WORKAROUND)
  float v = (s * fp64.ONE - a) * fp64.ONE;
  float err = (a - (s - v) * fp64.ONE) * fp64.ONE * fp64.ONE * fp64.ONE - (b + v);
#else
  float v = s - a;
  float err = (a - (s - v)) - (b + v);
#endif
  return vec2(s, err);
}

vec2 twoSqr(float a) {
  float prod = a * a;
  vec2 a_fp64 = split(a);
#if defined(LUMA_FP64_CODE_ELIMINATION_WORKAROUND)
  float err = ((a_fp64.x * a_fp64.x - prod) * fp64.ONE + 2.0 * a_fp64.x *
    a_fp64.y * fp64.ONE * fp64.ONE) + a_fp64.y * a_fp64.y * fp64.ONE * fp64.ONE * fp64.ONE;
#else
  float err = ((a_fp64.x * a_fp64.x - prod) + 2.0 * a_fp64.x * a_fp64.y) + a_fp64.y * a_fp64.y;
#endif
  return vec2(prod, err);
}

vec2 twoProd(float a, float b) {
  float prod = a * b;
  vec2 a_fp64 = split(a);
  vec2 b_fp64 = split(b);
  float err = ((a_fp64.x * b_fp64.x - prod) + a_fp64.x * b_fp64.y +
    a_fp64.y * b_fp64.x) + a_fp64.y * b_fp64.y;
  return vec2(prod, err);
}

vec2 sum_fp64(vec2 a, vec2 b) {
  vec2 s, t;
  s = twoSum(a.x, b.x);
  t = twoSum(a.y, b.y);
  s.y += t.x;
  s = quickTwoSum(s.x, s.y);
  s.y += t.y;
  s = quickTwoSum(s.x, s.y);
  return s;
}

vec2 sub_fp64(vec2 a, vec2 b) {
  vec2 s, t;
  s = twoSub(a.x, b.x);
  t = twoSub(a.y, b.y);
  s.y += t.x;
  s = quickTwoSum(s.x, s.y);
  s.y += t.y;
  s = quickTwoSum(s.x, s.y);
  return s;
}

vec2 mul_fp64(vec2 a, vec2 b) {
  vec2 prod = twoProd(a.x, b.x);
  // y component is for the error
  prod.y += a.x * b.y;
#if defined(LUMA_FP64_HIGH_BITS_OVERFLOW_WORKAROUND)
  prod = split2(prod);
#endif
  prod = quickTwoSum(prod.x, prod.y);
  prod.y += a.y * b.x;
#if defined(LUMA_FP64_HIGH_BITS_OVERFLOW_WORKAROUND)
  prod = split2(prod);
#endif
  prod = quickTwoSum(prod.x, prod.y);
  return prod;
}

vec2 div_fp64(vec2 a, vec2 b) {
  float xn = 1.0 / b.x;
#if defined(LUMA_FP64_HIGH_BITS_OVERFLOW_WORKAROUND)
  vec2 yn = mul_fp64(a, vec2(xn, 0));
#else
  vec2 yn = a * xn;
#endif
  float diff = (sub_fp64(a, mul_fp64(b, yn))).x;
  vec2 prod = twoProd(xn, diff);
  return sum_fp64(yn, prod);
}

vec2 sqrt_fp64(vec2 a) {
  if (a.x == 0.0 && a.y == 0.0) return vec2(0.0, 0.0);
  if (a.x < 0.0) return vec2(0.0 / 0.0, 0.0 / 0.0);

  float x = 1.0 / sqrt(a.x);
  float yn = a.x * x;
#if defined(LUMA_FP64_CODE_ELIMINATION_WORKAROUND)
  vec2 yn_sqr = twoSqr(yn) * fp64.ONE;
#else
  vec2 yn_sqr = twoSqr(yn);
#endif
  float diff = sub_fp64(a, yn_sqr).x;
  vec2 prod = twoProd(x * 0.5, diff);
#if defined(LUMA_FP64_HIGH_BITS_OVERFLOW_WORKAROUND)
  return sum_fp64(split(yn), prod);
#else
  return sum_fp64(vec2(yn, 0.0), prod);
#endif
}
`;var ur=`const vec2 E_FP64 = vec2(2.7182817459106445e+00, 8.254840366817007e-08);
const vec2 LOG2_FP64 = vec2(0.6931471824645996e+00, -1.9046542121259336e-09);
const vec2 PI_FP64 = vec2(3.1415927410125732, -8.742278012618954e-8);
const vec2 TWO_PI_FP64 = vec2(6.2831854820251465, -1.7484556025237907e-7);
const vec2 PI_2_FP64 = vec2(1.5707963705062866, -4.371139006309477e-8);
const vec2 PI_4_FP64 = vec2(0.7853981852531433, -2.1855695031547384e-8);
const vec2 PI_16_FP64 = vec2(0.19634954631328583, -5.463923757886846e-9);
const vec2 PI_16_2_FP64 = vec2(0.39269909262657166, -1.0927847515773692e-8);
const vec2 PI_16_3_FP64 = vec2(0.5890486240386963, -1.4906100798128818e-9);
const vec2 PI_180_FP64 = vec2(0.01745329238474369, 1.3519960498364902e-10);

const vec2 SIN_TABLE_0_FP64 = vec2(0.19509032368659973, -1.6704714833615242e-9);
const vec2 SIN_TABLE_1_FP64 = vec2(0.3826834261417389, 6.22335089017767e-9);
const vec2 SIN_TABLE_2_FP64 = vec2(0.5555702447891235, -1.1769521357507529e-8);
const vec2 SIN_TABLE_3_FP64 = vec2(0.7071067690849304, 1.2101617041793133e-8);

const vec2 COS_TABLE_0_FP64 = vec2(0.9807852506637573, 2.9739473106360492e-8);
const vec2 COS_TABLE_1_FP64 = vec2(0.9238795042037964, 2.8307490351764386e-8);
const vec2 COS_TABLE_2_FP64 = vec2(0.8314695954322815, 1.6870263741530778e-8);
const vec2 COS_TABLE_3_FP64 = vec2(0.7071067690849304, 1.2101617152815436e-8);

const vec2 INVERSE_FACTORIAL_3_FP64 = vec2(1.666666716337204e-01, -4.967053879312289e-09); // 1/3!
const vec2 INVERSE_FACTORIAL_4_FP64 = vec2(4.16666679084301e-02, -1.2417634698280722e-09); // 1/4!
const vec2 INVERSE_FACTORIAL_5_FP64 = vec2(8.333333767950535e-03, -4.34617203337595e-10); // 1/5!
const vec2 INVERSE_FACTORIAL_6_FP64 = vec2(1.3888889225199819e-03, -3.3631094437103215e-11); // 1/6!
const vec2 INVERSE_FACTORIAL_7_FP64 = vec2(1.9841270113829523e-04,  -2.725596874933456e-12); // 1/7!
const vec2 INVERSE_FACTORIAL_8_FP64 = vec2(2.4801587642286904e-05, -3.406996025904184e-13); // 1/8!
const vec2 INVERSE_FACTORIAL_9_FP64 = vec2(2.75573188446287533e-06, 3.7935713937038186e-14); // 1/9!
const vec2 INVERSE_FACTORIAL_10_FP64 = vec2(2.755731998149713e-07, -7.575112367869873e-15); // 1/10!

float nint(float d) {
    if (d == floor(d)) return d;
    return floor(d + 0.5);
}

vec2 nint_fp64(vec2 a) {
    float hi = nint(a.x);
    float lo;
    vec2 tmp;
    if (hi == a.x) {
        lo = nint(a.y);
        tmp = quickTwoSum(hi, lo);
    } else {
        lo = 0.0;
        if (abs(hi - a.x) == 0.5 && a.y < 0.0) {
            hi -= 1.0;
        }
        tmp = vec2(hi, lo);
    }
    return tmp;
}

/* k_power controls how much range reduction we would like to have
Range reduction uses the following method:
assume a = k_power * r + m * log(2), k and m being integers.
Set k_power = 4 (we can choose other k to trade accuracy with performance.
we only need to calculate exp(r) and using exp(a) = 2^m * exp(r)^k_power;
*/

vec2 exp_fp64(vec2 a) {
  // We need to make sure these two numbers match
  // as bit-wise shift is not available in GLSL 1.0
  const int k_power = 4;
  const float k = 16.0;

  const float inv_k = 1.0 / k;

  if (a.x <= -88.0) return vec2(0.0, 0.0);
  if (a.x >= 88.0) return vec2(1.0 / 0.0, 1.0 / 0.0);
  if (a.x == 0.0 && a.y == 0.0) return vec2(1.0, 0.0);
  if (a.x == 1.0 && a.y == 0.0) return E_FP64;

  float m = floor(a.x / LOG2_FP64.x + 0.5);
  vec2 r = sub_fp64(a, mul_fp64(LOG2_FP64, vec2(m, 0.0))) * inv_k;
  vec2 s, t, p;

  p = mul_fp64(r, r);
  s = sum_fp64(r, p * 0.5);
  p = mul_fp64(p, r);
  t = mul_fp64(p, INVERSE_FACTORIAL_3_FP64);

  s = sum_fp64(s, t);
  p = mul_fp64(p, r);
  t = mul_fp64(p, INVERSE_FACTORIAL_4_FP64);

  s = sum_fp64(s, t);
  p = mul_fp64(p, r);
  t = mul_fp64(p, INVERSE_FACTORIAL_5_FP64);

  // s = sum_fp64(s, t);
  // p = mul_fp64(p, r);
  // t = mul_fp64(p, INVERSE_FACTORIAL_6_FP64);

  // s = sum_fp64(s, t);
  // p = mul_fp64(p, r);
  // t = mul_fp64(p, INVERSE_FACTORIAL_7_FP64);

  s = sum_fp64(s, t);


  // At this point, s = exp(r) - 1; but after following 4 recursions, we will get exp(r) ^ 512 - 1.
  for (int i = 0; i < k_power; i++) {
    s = sum_fp64(s * 2.0, mul_fp64(s, s));
  }

#if defined(NVIDIA_FP64_WORKAROUND) || defined(INTEL_FP64_WORKAROUND)
  s = sum_fp64(s, vec2(fp64.ONE, 0.0));
#else
  s = sum_fp64(s, vec2(1.0, 0.0));
#endif

  return s * pow(2.0, m);
//   return r;
}

vec2 log_fp64(vec2 a)
{
  if (a.x == 1.0 && a.y == 0.0) return vec2(0.0, 0.0);
  if (a.x <= 0.0) return vec2(0.0 / 0.0, 0.0 / 0.0);
  vec2 x = vec2(log(a.x), 0.0);
  vec2 s;
#if defined(NVIDIA_FP64_WORKAROUND) || defined(INTEL_FP64_WORKAROUND)
  s = vec2(fp64.ONE, 0.0);
#else
  s = vec2(1.0, 0.0);
#endif

  x = sub_fp64(sum_fp64(x, mul_fp64(a, exp_fp64(-x))), s);
  return x;
}

vec2 sin_taylor_fp64(vec2 a) {
  vec2 r, s, t, x;

  if (a.x == 0.0 && a.y == 0.0) {
    return vec2(0.0, 0.0);
  }

  x = -mul_fp64(a, a);
  s = a;
  r = a;

  r = mul_fp64(r, x);
  t = mul_fp64(r, INVERSE_FACTORIAL_3_FP64);
  s = sum_fp64(s, t);

  r = mul_fp64(r, x);
  t = mul_fp64(r, INVERSE_FACTORIAL_5_FP64);
  s = sum_fp64(s, t);

  /* keep the following commented code in case we need them
  for extra accuracy from the Taylor expansion*/

  // r = mul_fp64(r, x);
  // t = mul_fp64(r, INVERSE_FACTORIAL_7_FP64);
  // s = sum_fp64(s, t);

  // r = mul_fp64(r, x);
  // t = mul_fp64(r, INVERSE_FACTORIAL_9_FP64);
  // s = sum_fp64(s, t);

  return s;
}

vec2 cos_taylor_fp64(vec2 a) {
  vec2 r, s, t, x;

  if (a.x == 0.0 && a.y == 0.0) {
    return vec2(1.0, 0.0);
  }

  x = -mul_fp64(a, a);
  r = x;
  s = sum_fp64(vec2(1.0, 0.0), r * 0.5);

  r = mul_fp64(r, x);
  t = mul_fp64(r, INVERSE_FACTORIAL_4_FP64);
  s = sum_fp64(s, t);

  r = mul_fp64(r, x);
  t = mul_fp64(r, INVERSE_FACTORIAL_6_FP64);
  s = sum_fp64(s, t);

  /* keep the following commented code in case we need them
  for extra accuracy from the Taylor expansion*/

  // r = mul_fp64(r, x);
  // t = mul_fp64(r, INVERSE_FACTORIAL_8_FP64);
  // s = sum_fp64(s, t);

  // r = mul_fp64(r, x);
  // t = mul_fp64(r, INVERSE_FACTORIAL_10_FP64);
  // s = sum_fp64(s, t);

  return s;
}

void sincos_taylor_fp64(vec2 a, out vec2 sin_t, out vec2 cos_t) {
  if (a.x == 0.0 && a.y == 0.0) {
    sin_t = vec2(0.0, 0.0);
    cos_t = vec2(1.0, 0.0);
  }

  sin_t = sin_taylor_fp64(a);
  cos_t = sqrt_fp64(sub_fp64(vec2(1.0, 0.0), mul_fp64(sin_t, sin_t)));
}

vec2 sin_fp64(vec2 a) {
    if (a.x == 0.0 && a.y == 0.0) {
        return vec2(0.0, 0.0);
    }

    // 2pi range reduction
    vec2 z = nint_fp64(div_fp64(a, TWO_PI_FP64));
    vec2 r = sub_fp64(a, mul_fp64(TWO_PI_FP64, z));

    vec2 t;
    float q = floor(r.x / PI_2_FP64.x + 0.5);
    int j = int(q);

    if (j < -2 || j > 2) {
        return vec2(0.0 / 0.0, 0.0 / 0.0);
    }

    t = sub_fp64(r, mul_fp64(PI_2_FP64, vec2(q, 0.0)));

    q = floor(t.x / PI_16_FP64.x + 0.5);
    int k = int(q);

    if (k == 0) {
        if (j == 0) {
            return sin_taylor_fp64(t);
        } else if (j == 1) {
            return cos_taylor_fp64(t);
        } else if (j == -1) {
            return -cos_taylor_fp64(t);
        } else {
            return -sin_taylor_fp64(t);
        }
    }

    int abs_k = int(abs(float(k)));

    if (abs_k > 4) {
        return vec2(0.0 / 0.0, 0.0 / 0.0);
    } else {
        t = sub_fp64(t, mul_fp64(PI_16_FP64, vec2(q, 0.0)));
    }

    vec2 u = vec2(0.0, 0.0);
    vec2 v = vec2(0.0, 0.0);

#if defined(NVIDIA_FP64_WORKAROUND) || defined(INTEL_FP64_WORKAROUND)
    if (abs(float(abs_k) - 1.0) < 0.5) {
        u = COS_TABLE_0_FP64;
        v = SIN_TABLE_0_FP64;
    } else if (abs(float(abs_k) - 2.0) < 0.5) {
        u = COS_TABLE_1_FP64;
        v = SIN_TABLE_1_FP64;
    } else if (abs(float(abs_k) - 3.0) < 0.5) {
        u = COS_TABLE_2_FP64;
        v = SIN_TABLE_2_FP64;
    } else if (abs(float(abs_k) - 4.0) < 0.5) {
        u = COS_TABLE_3_FP64;
        v = SIN_TABLE_3_FP64;
    }
#else
    if (abs_k == 1) {
        u = COS_TABLE_0_FP64;
        v = SIN_TABLE_0_FP64;
    } else if (abs_k == 2) {
        u = COS_TABLE_1_FP64;
        v = SIN_TABLE_1_FP64;
    } else if (abs_k == 3) {
        u = COS_TABLE_2_FP64;
        v = SIN_TABLE_2_FP64;
    } else if (abs_k == 4) {
        u = COS_TABLE_3_FP64;
        v = SIN_TABLE_3_FP64;
    }
#endif

    vec2 sin_t, cos_t;
    sincos_taylor_fp64(t, sin_t, cos_t);



    vec2 result = vec2(0.0, 0.0);
    if (j == 0) {
        if (k > 0) {
            result = sum_fp64(mul_fp64(u, sin_t), mul_fp64(v, cos_t));
        } else {
            result = sub_fp64(mul_fp64(u, sin_t), mul_fp64(v, cos_t));
        }
    } else if (j == 1) {
        if (k > 0) {
            result = sub_fp64(mul_fp64(u, cos_t), mul_fp64(v, sin_t));
        } else {
            result = sum_fp64(mul_fp64(u, cos_t), mul_fp64(v, sin_t));
        }
    } else if (j == -1) {
        if (k > 0) {
            result = sub_fp64(mul_fp64(v, sin_t), mul_fp64(u, cos_t));
        } else {
            result = -sum_fp64(mul_fp64(v, sin_t), mul_fp64(u, cos_t));
        }
    } else {
        if (k > 0) {
            result = -sum_fp64(mul_fp64(u, sin_t), mul_fp64(v, cos_t));
        } else {
            result = sub_fp64(mul_fp64(v, cos_t), mul_fp64(u, sin_t));
        }
    }

    return result;
}

vec2 cos_fp64(vec2 a) {
    if (a.x == 0.0 && a.y == 0.0) {
        return vec2(1.0, 0.0);
    }

    // 2pi range reduction
    vec2 z = nint_fp64(div_fp64(a, TWO_PI_FP64));
    vec2 r = sub_fp64(a, mul_fp64(TWO_PI_FP64, z));

    vec2 t;
    float q = floor(r.x / PI_2_FP64.x + 0.5);
    int j = int(q);

    if (j < -2 || j > 2) {
        return vec2(0.0 / 0.0, 0.0 / 0.0);
    }

    t = sub_fp64(r, mul_fp64(PI_2_FP64, vec2(q, 0.0)));

    q = floor(t.x / PI_16_FP64.x + 0.5);
    int k = int(q);

    if (k == 0) {
        if (j == 0) {
            return cos_taylor_fp64(t);
        } else if (j == 1) {
            return -sin_taylor_fp64(t);
        } else if (j == -1) {
            return sin_taylor_fp64(t);
        } else {
            return -cos_taylor_fp64(t);
        }
    }

    int abs_k = int(abs(float(k)));

    if (abs_k > 4) {
        return vec2(0.0 / 0.0, 0.0 / 0.0);
    } else {
        t = sub_fp64(t, mul_fp64(PI_16_FP64, vec2(q, 0.0)));
    }

    vec2 u = vec2(0.0, 0.0);
    vec2 v = vec2(0.0, 0.0);

#if defined(NVIDIA_FP64_WORKAROUND) || defined(INTEL_FP64_WORKAROUND)
    if (abs(float(abs_k) - 1.0) < 0.5) {
        u = COS_TABLE_0_FP64;
        v = SIN_TABLE_0_FP64;
    } else if (abs(float(abs_k) - 2.0) < 0.5) {
        u = COS_TABLE_1_FP64;
        v = SIN_TABLE_1_FP64;
    } else if (abs(float(abs_k) - 3.0) < 0.5) {
        u = COS_TABLE_2_FP64;
        v = SIN_TABLE_2_FP64;
    } else if (abs(float(abs_k) - 4.0) < 0.5) {
        u = COS_TABLE_3_FP64;
        v = SIN_TABLE_3_FP64;
    }
#else
    if (abs_k == 1) {
        u = COS_TABLE_0_FP64;
        v = SIN_TABLE_0_FP64;
    } else if (abs_k == 2) {
        u = COS_TABLE_1_FP64;
        v = SIN_TABLE_1_FP64;
    } else if (abs_k == 3) {
        u = COS_TABLE_2_FP64;
        v = SIN_TABLE_2_FP64;
    } else if (abs_k == 4) {
        u = COS_TABLE_3_FP64;
        v = SIN_TABLE_3_FP64;
    }
#endif

    vec2 sin_t, cos_t;
    sincos_taylor_fp64(t, sin_t, cos_t);

    vec2 result = vec2(0.0, 0.0);
    if (j == 0) {
        if (k > 0) {
            result = sub_fp64(mul_fp64(u, cos_t), mul_fp64(v, sin_t));
        } else {
            result = sum_fp64(mul_fp64(u, cos_t), mul_fp64(v, sin_t));
        }
    } else if (j == 1) {
        if (k > 0) {
            result = -sum_fp64(mul_fp64(u, sin_t), mul_fp64(v, cos_t));
        } else {
            result = sub_fp64(mul_fp64(v, cos_t), mul_fp64(u, sin_t));
        }
    } else if (j == -1) {
        if (k > 0) {
            result = sum_fp64(mul_fp64(u, sin_t), mul_fp64(v, cos_t));
        } else {
            result = sub_fp64(mul_fp64(u, sin_t), mul_fp64(v, cos_t));
        }
    } else {
        if (k > 0) {
            result = sub_fp64(mul_fp64(v, sin_t), mul_fp64(u, cos_t));
        } else {
            result = -sum_fp64(mul_fp64(u, cos_t), mul_fp64(v, sin_t));
        }
    }

    return result;
}

vec2 tan_fp64(vec2 a) {
    vec2 sin_a;
    vec2 cos_a;

    if (a.x == 0.0 && a.y == 0.0) {
        return vec2(0.0, 0.0);
    }

    // 2pi range reduction
    vec2 z = nint_fp64(div_fp64(a, TWO_PI_FP64));
    vec2 r = sub_fp64(a, mul_fp64(TWO_PI_FP64, z));

    vec2 t;
    float q = floor(r.x / PI_2_FP64.x + 0.5);
    int j = int(q);


    if (j < -2 || j > 2) {
        return vec2(0.0 / 0.0, 0.0 / 0.0);
    }

    t = sub_fp64(r, mul_fp64(PI_2_FP64, vec2(q, 0.0)));

    q = floor(t.x / PI_16_FP64.x + 0.5);
    int k = int(q);
    int abs_k = int(abs(float(k)));

    // We just can't get PI/16 * 3.0 very accurately.
    // so let's just store it
    if (abs_k > 4) {
        return vec2(0.0 / 0.0, 0.0 / 0.0);
    } else {
        t = sub_fp64(t, mul_fp64(PI_16_FP64, vec2(q, 0.0)));
    }


    vec2 u = vec2(0.0, 0.0);
    vec2 v = vec2(0.0, 0.0);

    vec2 sin_t, cos_t;
    vec2 s, c;
    sincos_taylor_fp64(t, sin_t, cos_t);

    if (k == 0) {
        s = sin_t;
        c = cos_t;
    } else {
#if defined(NVIDIA_FP64_WORKAROUND) || defined(INTEL_FP64_WORKAROUND)
        if (abs(float(abs_k) - 1.0) < 0.5) {
            u = COS_TABLE_0_FP64;
            v = SIN_TABLE_0_FP64;
        } else if (abs(float(abs_k) - 2.0) < 0.5) {
            u = COS_TABLE_1_FP64;
            v = SIN_TABLE_1_FP64;
        } else if (abs(float(abs_k) - 3.0) < 0.5) {
            u = COS_TABLE_2_FP64;
            v = SIN_TABLE_2_FP64;
        } else if (abs(float(abs_k) - 4.0) < 0.5) {
            u = COS_TABLE_3_FP64;
            v = SIN_TABLE_3_FP64;
        }
#else
        if (abs_k == 1) {
            u = COS_TABLE_0_FP64;
            v = SIN_TABLE_0_FP64;
        } else if (abs_k == 2) {
            u = COS_TABLE_1_FP64;
            v = SIN_TABLE_1_FP64;
        } else if (abs_k == 3) {
            u = COS_TABLE_2_FP64;
            v = SIN_TABLE_2_FP64;
        } else if (abs_k == 4) {
            u = COS_TABLE_3_FP64;
            v = SIN_TABLE_3_FP64;
        }
#endif
        if (k > 0) {
            s = sum_fp64(mul_fp64(u, sin_t), mul_fp64(v, cos_t));
            c = sub_fp64(mul_fp64(u, cos_t), mul_fp64(v, sin_t));
        } else {
            s = sub_fp64(mul_fp64(u, sin_t), mul_fp64(v, cos_t));
            c = sum_fp64(mul_fp64(u, cos_t), mul_fp64(v, sin_t));
        }
    }

    if (j == 0) {
        sin_a = s;
        cos_a = c;
    } else if (j == 1) {
        sin_a = c;
        cos_a = -s;
    } else if (j == -1) {
        sin_a = -c;
        cos_a = s;
    } else {
        sin_a = -s;
        cos_a = -c;
    }
    return div_fp64(sin_a, cos_a);
}

vec2 radians_fp64(vec2 degree) {
  return mul_fp64(degree, PI_180_FP64);
}

vec2 mix_fp64(vec2 a, vec2 b, float x) {
  vec2 range = sub_fp64(b, a);
  return sum_fp64(a, mul_fp64(range, vec2(x, 0.0)));
}

// Vector functions
// vec2 functions
void vec2_sum_fp64(vec2 a[2], vec2 b[2], out vec2 out_val[2]) {
    out_val[0] = sum_fp64(a[0], b[0]);
    out_val[1] = sum_fp64(a[1], b[1]);
}

void vec2_sub_fp64(vec2 a[2], vec2 b[2], out vec2 out_val[2]) {
    out_val[0] = sub_fp64(a[0], b[0]);
    out_val[1] = sub_fp64(a[1], b[1]);
}

void vec2_mul_fp64(vec2 a[2], vec2 b[2], out vec2 out_val[2]) {
    out_val[0] = mul_fp64(a[0], b[0]);
    out_val[1] = mul_fp64(a[1], b[1]);
}

void vec2_div_fp64(vec2 a[2], vec2 b[2], out vec2 out_val[2]) {
    out_val[0] = div_fp64(a[0], b[0]);
    out_val[1] = div_fp64(a[1], b[1]);
}

void vec2_mix_fp64(vec2 x[2], vec2 y[2], float a, out vec2 out_val[2]) {
  vec2 range[2];
  vec2_sub_fp64(y, x, range);
  vec2 portion[2];
  portion[0] = range[0] * a;
  portion[1] = range[1] * a;
  vec2_sum_fp64(x, portion, out_val);
}

vec2 vec2_length_fp64(vec2 x[2]) {
  return sqrt_fp64(sum_fp64(mul_fp64(x[0], x[0]), mul_fp64(x[1], x[1])));
}

void vec2_normalize_fp64(vec2 x[2], out vec2 out_val[2]) {
  vec2 length = vec2_length_fp64(x);
  vec2 length_vec2[2];
  length_vec2[0] = length;
  length_vec2[1] = length;

  vec2_div_fp64(x, length_vec2, out_val);
}

vec2 vec2_distance_fp64(vec2 x[2], vec2 y[2]) {
  vec2 diff[2];
  vec2_sub_fp64(x, y, diff);
  return vec2_length_fp64(diff);
}

vec2 vec2_dot_fp64(vec2 a[2], vec2 b[2]) {
  vec2 v[2];

  v[0] = mul_fp64(a[0], b[0]);
  v[1] = mul_fp64(a[1], b[1]);

  return sum_fp64(v[0], v[1]);
}

// vec3 functions
void vec3_sub_fp64(vec2 a[3], vec2 b[3], out vec2 out_val[3]) {
  for (int i = 0; i < 3; i++) {
    out_val[i] = sum_fp64(a[i], b[i]);
  }
}

void vec3_sum_fp64(vec2 a[3], vec2 b[3], out vec2 out_val[3]) {
  for (int i = 0; i < 3; i++) {
    out_val[i] = sum_fp64(a[i], b[i]);
  }
}

vec2 vec3_length_fp64(vec2 x[3]) {
  return sqrt_fp64(sum_fp64(sum_fp64(mul_fp64(x[0], x[0]), mul_fp64(x[1], x[1])),
    mul_fp64(x[2], x[2])));
}

vec2 vec3_distance_fp64(vec2 x[3], vec2 y[3]) {
  vec2 diff[3];
  vec3_sub_fp64(x, y, diff);
  return vec3_length_fp64(diff);
}

// vec4 functions
void vec4_fp64(vec4 a, out vec2 out_val[4]) {
  out_val[0].x = a[0];
  out_val[0].y = 0.0;

  out_val[1].x = a[1];
  out_val[1].y = 0.0;

  out_val[2].x = a[2];
  out_val[2].y = 0.0;

  out_val[3].x = a[3];
  out_val[3].y = 0.0;
}

void vec4_scalar_mul_fp64(vec2 a[4], vec2 b, out vec2 out_val[4]) {
  out_val[0] = mul_fp64(a[0], b);
  out_val[1] = mul_fp64(a[1], b);
  out_val[2] = mul_fp64(a[2], b);
  out_val[3] = mul_fp64(a[3], b);
}

void vec4_sum_fp64(vec2 a[4], vec2 b[4], out vec2 out_val[4]) {
  for (int i = 0; i < 4; i++) {
    out_val[i] = sum_fp64(a[i], b[i]);
  }
}

void vec4_dot_fp64(vec2 a[4], vec2 b[4], out vec2 out_val) {
  vec2 v[4];

  v[0] = mul_fp64(a[0], b[0]);
  v[1] = mul_fp64(a[1], b[1]);
  v[2] = mul_fp64(a[2], b[2]);
  v[3] = mul_fp64(a[3], b[3]);

  out_val = sum_fp64(sum_fp64(v[0], v[1]), sum_fp64(v[2], v[3]));
}

void mat4_vec4_mul_fp64(vec2 b[16], vec2 a[4], out vec2 out_val[4]) {
  vec2 tmp[4];

  for (int i = 0; i < 4; i++)
  {
    for (int j = 0; j < 4; j++)
    {
      tmp[j] = b[j + i * 4];
    }
    vec4_dot_fp64(a, tmp, out_val[i]);
  }
}
`;var ds={ONE:1},hn={name:"fp64arithmetic",vs:cr,defaultUniforms:ds,uniformTypes:{ONE:"f32"},fp64ify:Te,fp64LowPart:je,fp64ifyMatrix4:Xe},fr={name:"fp64",vs:ur,dependencies:[hn],fp64ify:Te,fp64LowPart:je,fp64ifyMatrix4:Xe};var ms=[0,1,1,1],_s=`uniform pickingUniforms {
  float isActive;
  float isAttribute;
  float isHighlightActive;
  float useFloatColors;
  vec3 highlightedObjectColor;
  vec4 highlightColor;
} picking;

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
`,gs=`uniform pickingUniforms {
  float isActive;
  float isAttribute;
  float isHighlightActive;
  float useFloatColors;
  vec3 highlightedObjectColor;
  vec4 highlightColor;
} picking;

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
`,hr={props:{},uniforms:{},name:"picking",uniformTypes:{isActive:"f32",isAttribute:"f32",isHighlightActive:"f32",useFloatColors:"f32",highlightedObjectColor:"vec3<f32>",highlightColor:"vec4<f32>"},defaultUniforms:{isActive:!1,isAttribute:!1,isHighlightActive:!1,useFloatColors:!0,highlightedObjectColor:[0,0,0],highlightColor:ms},vs:_s,fs:gs,getUniforms:vs};function vs(a={},e){let t={};if(a.highlightedObjectColor!==void 0)if(a.highlightedObjectColor===null)t.isHighlightActive=!1;else{t.isHighlightActive=!0;let n=a.highlightedObjectColor.slice(0,3);t.highlightedObjectColor=n}if(a.highlightColor){let n=Array.from(a.highlightColor,r=>r/255);Number.isFinite(n[3])||(n[3]=1),t.highlightColor=n}return a.isActive!==void 0&&(t.isActive=Boolean(a.isActive),t.isAttribute=Boolean(a.isAttribute)),a.useFloatColors!==void 0&&(t.useFloatColors=Boolean(a.useFloatColors)),t}var dr=Rt(Je(),1);var pn=`precision highp int;

// #if (defined(SHADER_TYPE_FRAGMENT) && defined(LIGHTING_FRAGMENT)) || (defined(SHADER_TYPE_VERTEX) && defined(LIGHTING_VERTEX))
struct AmbientLight {
  vec3 color;
};

struct PointLight {
  vec3 color;
  vec3 position;
  vec3 attenuation; // 2nd order x:Constant-y:Linear-z:Exponential
};

struct DirectionalLight {
  vec3 color;
  vec3 direction;
};

uniform lightingUniforms {
  int enabled;
  int lightType;

  int directionalLightCount;
  int pointLightCount;

  vec3 ambientColor;

  vec3 lightColor0;
  vec3 lightPosition0;
  vec3 lightDirection0;
  vec3 lightAttenuation0;

  vec3 lightColor1;
  vec3 lightPosition1;
  vec3 lightDirection1;
  vec3 lightAttenuation1;

  vec3 lightColor2;
  vec3 lightPosition2;
  vec3 lightDirection2;
  vec3 lightAttenuation2;
} lighting;

PointLight lighting_getPointLight(int index) {
  switch (index) {
    case 0:
      return PointLight(lighting.lightColor0, lighting.lightPosition0, lighting.lightAttenuation0);
    case 1:
      return PointLight(lighting.lightColor1, lighting.lightPosition1, lighting.lightAttenuation1);
    case 2:
    default:  
      return PointLight(lighting.lightColor2, lighting.lightPosition2, lighting.lightAttenuation2);
  }
}

DirectionalLight lighting_getDirectionalLight(int index) {
  switch (index) {
    case 0:
      return DirectionalLight(lighting.lightColor0, lighting.lightDirection0);
    case 1:
      return DirectionalLight(lighting.lightColor1, lighting.lightDirection1);
    case 2:
    default:   
      return DirectionalLight(lighting.lightColor2, lighting.lightDirection2);
  }
} 

float getPointLightAttenuation(PointLight pointLight, float distance) {
  return pointLight.attenuation.x
       + pointLight.attenuation.y * distance
       + pointLight.attenuation.z * distance * distance;
}

// #endif
`;var pr=`// #if (defined(SHADER_TYPE_FRAGMENT) && defined(LIGHTING_FRAGMENT)) || (defined(SHADER_TYPE_VERTEX) && defined(LIGHTING_VERTEX))
struct AmbientLight {
  color: vec3<f32>,
};

struct PointLight {
  color: vec3<f32>,
  position: vec3<f32>,
  attenuation: vec3<f32>, // 2nd order x:Constant-y:Linear-z:Exponential
};

struct DirectionalLight {
  color: vec3<f32>,
  direction: vec3<f32>,
};

struct lightingUniforms {
  enabled: i32,
  pointLightCount: i32,
  directionalLightCount: i32,

  ambientColor: vec3<f32>,

  // TODO - support multiple lights by uncommenting arrays below
  lightType: i32,
  lightColor: vec3<f32>,
  lightDirection: vec3<f32>,
  lightPosition: vec3<f32>,
  lightAttenuation: vec3<f32>,

  // AmbientLight ambientLight;
  // PointLight pointLight[MAX_LIGHTS];
  // DirectionalLight directionalLight[MAX_LIGHTS];
};

// Binding 0:1 is reserved for lighting (Note: could go into separate bind group as it is stable across draw calls)
@binding(1) @group(0) var<uniform> lighting : lightingUniforms;

fn lighting_getPointLight(index: i32) -> PointLight {
  return PointLight(lighting.lightColor, lighting.lightPosition, lighting.lightAttenuation);
}

fn lighting_getDirectionalLight(index: i32) -> DirectionalLight {
  return DirectionalLight(lighting.lightColor, lighting.lightDirection);
} 

fn getPointLightAttenuation(pointLight: PointLight, distance: f32) -> f32 {
  return pointLight.attenuation.x
       + pointLight.attenuation.y * distance
       + pointLight.attenuation.z * distance * distance;
}
`;var bs=5,xs=255,Ke;(function(a){a[a.POINT=0]="POINT",a[a.DIRECTIONAL=1]="DIRECTIONAL"})(Ke||(Ke={}));var te={props:{},uniforms:{},name:"lighting",defines:{},uniformTypes:{enabled:"i32",lightType:"i32",directionalLightCount:"i32",pointLightCount:"i32",ambientColor:"vec3<f32>",lightColor0:"vec3<f32>",lightPosition0:"vec3<f32>",lightDirection0:"vec3<f32>",lightAttenuation0:"vec3<f32>",lightColor1:"vec3<f32>",lightPosition1:"vec3<f32>",lightDirection1:"vec3<f32>",lightAttenuation1:"vec3<f32>",lightColor2:"vec3<f32>",lightPosition2:"vec3<f32>",lightDirection2:"vec3<f32>",lightAttenuation2:"vec3<f32>"},defaultUniforms:{enabled:1,lightType:Ke.POINT,directionalLightCount:0,pointLightCount:0,ambientColor:[.1,.1,.1],lightColor0:[1,1,1],lightPosition0:[1,1,2],lightDirection0:[1,1,1],lightAttenuation0:[1,0,0],lightColor1:[1,1,1],lightPosition1:[1,1,2],lightDirection1:[1,1,1],lightAttenuation1:[1,0,0],lightColor2:[1,1,1],lightPosition2:[1,1,2],lightDirection2:[1,1,1],lightAttenuation2:[1,0,0]},source:pr,vs:pn,fs:pn,getUniforms:ys};function ys(a,e={}){if(a=a&&{...a},!a)return{...te.defaultUniforms};a.lights&&(a={...a,...ks(a.lights),lights:void 0});let{ambientLight:t,pointLights:n,directionalLights:r}=a||{};if(!(t||n&&n.length>0||r&&r.length>0))return{...te.defaultUniforms,enabled:0};let i={...te.defaultUniforms,...e,...ws({ambientLight:t,pointLights:n,directionalLights:r})};return a.enabled!==void 0&&(i.enabled=a.enabled?1:0),i}function ws({ambientLight:a,pointLights:e=[],directionalLights:t=[]}){let n={};n.ambientColor=dn(a);let r=0;for(let s of e){n.lightType=Ke.POINT;let i=r;n[`lightColor${i}`]=dn(s),n[`lightPosition${i}`]=s.position,n[`lightAttenuation${i}`]=s.attenuation||[1,0,0],r++}for(let s of t){n.lightType=Ke.DIRECTIONAL;let i=r;n[`lightColor${i}`]=dn(s),n[`lightDirection${i}`]=s.direction,r++}return r>bs&&dr.log.warn("MAX_LIGHTS exceeded")(),n.directionalLightCount=t.length,n.pointLightCount=e.length,n}function ks(a){let e={pointLights:[],directionalLights:[]};for(let t of a||[])switch(t.type){case"ambient":e.ambientLight=t;break;case"directional":e.directionalLights?.push(t);break;case"point":e.pointLights?.push(t);break;default:}return e}function dn(a={}){let{color:e=[0,0,0],intensity:t=1}=a;return e.map(n=>n*t/xs)}var Is=`  
struct dirlightUniforms {
  lightDirection: vec3<f32>,
};

alias DirlightNormal = vec3<f32>;

struct DirlightInputs {
  normal: DirlightNormal,
};

@binding(1) @group(0) var<uniform> dirlight : dirlightUniforms;

// For vertex
fn dirlight_setNormal(normal: vec3<f32>) -> DirlightNormal {
  return normalize(normal);
}

// Returns color attenuated by angle from light source
fn dirlight_filterColor(color: vec4<f32>, inputs: DirlightInputs) -> vec4<f32> {
  // TODO - fix default light direction
  // let lightDirection = dirlight.lightDirection;
  let lightDirection = vec3<f32>(1, 1, 1);
  let d: f32 = abs(dot(inputs.normal, normalize(lightDirection)));
  return vec4<f32>(color.rgb * d, color.a);
}
`,Ss=`out vec3 dirlight_vNormal;

void dirlight_setNormal(vec3 normal) {
  dirlight_vNormal = normalize(normal);
}
`,As=`uniform dirlightUniforms {
  vec3 lightDirection;
} dirlight;

in vec3 dirlight_vNormal;

// Returns color attenuated by angle from light source
vec4 dirlight_filterColor(vec4 color) {
  float d = abs(dot(dirlight_vNormal, normalize(dirlight.lightDirection)));
  return vec4(color.rgb * d, color.a);
}
`,mn={props:{},uniforms:{},name:"dirlight",dependencies:[],source:Is,vs:Ss,fs:As,uniformTypes:{lightDirection:"vec3<f32>"},defaultUniforms:{lightDirection:[1,1,2]},getUniforms:Ts};function Ts(a=mn.defaultUniforms){let e={};return a.lightDirection&&(e.lightDirection=a.lightDirection),e}var Pt=`uniform phongMaterialUniforms {
  uniform float ambient;
  uniform float diffuse;
  uniform float shininess;
  uniform vec3  specularColor;
} material;
`,Ft=`#define MAX_LIGHTS 3

uniform phongMaterialUniforms {
  uniform float ambient;
  uniform float diffuse;
  uniform float shininess;
  uniform vec3  specularColor;
} material;

vec3 lighting_getLightColor(vec3 surfaceColor, vec3 light_direction, vec3 view_direction, vec3 normal_worldspace, vec3 color) {
  vec3 halfway_direction = normalize(light_direction + view_direction);
  float lambertian = dot(light_direction, normal_worldspace);
  float specular = 0.0;
  if (lambertian > 0.0) {
    float specular_angle = max(dot(normal_worldspace, halfway_direction), 0.0);
    specular = pow(specular_angle, material.shininess);
  }
  lambertian = max(lambertian, 0.0);
  return (lambertian * material.diffuse * surfaceColor + specular * material.specularColor) * color;
}

vec3 lighting_getLightColor(vec3 surfaceColor, vec3 cameraPosition, vec3 position_worldspace, vec3 normal_worldspace) {
  vec3 lightColor = surfaceColor;

  if (lighting.enabled == 0) {
    return lightColor;
  }

  vec3 view_direction = normalize(cameraPosition - position_worldspace);
  lightColor = material.ambient * surfaceColor * lighting.ambientColor;

  for (int i = 0; i < lighting.pointLightCount; i++) {
    PointLight pointLight = lighting_getPointLight(i);
    vec3 light_position_worldspace = pointLight.position;
    vec3 light_direction = normalize(light_position_worldspace - position_worldspace);
    float light_attenuation = getPointLightAttenuation(pointLight, distance(light_position_worldspace, position_worldspace));
    lightColor += lighting_getLightColor(surfaceColor, light_direction, view_direction, normal_worldspace, pointLight.color / light_attenuation);
  }

  int totalLights = min(MAX_LIGHTS, lighting.pointLightCount + lighting.directionalLightCount);
  for (int i = lighting.pointLightCount; i < totalLights; i++) {
    DirectionalLight directionalLight = lighting_getDirectionalLight(i);
    lightColor += lighting_getLightColor(surfaceColor, -directionalLight.direction, view_direction, normal_worldspace, directionalLight.color);
  }
  
  return lightColor;
}
`;var Dt=`struct phongMaterialUniforms {
  ambient: f32,
  diffuse: f32,
  shininess: f32,
  specularColor: vec3<f32>,
};

@binding(2) @group(0) var<uniform> phongMaterial : phongMaterialUniforms;

fn lighting_getLightColor(surfaceColor: vec3<f32>, light_direction: vec3<f32>, view_direction: vec3<f32>, normal_worldspace: vec3<f32>, color: vec3<f32>) -> vec3<f32> {
  let halfway_direction: vec3<f32> = normalize(light_direction + view_direction);
  var lambertian: f32 = dot(light_direction, normal_worldspace);
  var specular: f32 = 0.0;
  if (lambertian > 0.0) {
    let specular_angle = max(dot(normal_worldspace, halfway_direction), 0.0);
    specular = pow(specular_angle, phongMaterial.shininess);
  }
  lambertian = max(lambertian, 0.0);
  return (lambertian * phongMaterial.diffuse * surfaceColor + specular * phongMaterial.specularColor) * color;
}

fn lighting_getLightColor2(surfaceColor: vec3<f32>, cameraPosition: vec3<f32>, position_worldspace: vec3<f32>, normal_worldspace: vec3<f32>) -> vec3<f32> {
  var lightColor: vec3<f32> = surfaceColor;

  if (lighting.enabled == 0) {
    return lightColor;
  }

  let view_direction: vec3<f32> = normalize(cameraPosition - position_worldspace);
  lightColor = phongMaterial.ambient * surfaceColor * lighting.ambientColor;

  if (lighting.lightType == 0) {
    let pointLight: PointLight  = lighting_getPointLight(0);
    let light_position_worldspace: vec3<f32> = pointLight.position;
    let light_direction: vec3<f32> = normalize(light_position_worldspace - position_worldspace);
    lightColor += lighting_getLightColor(surfaceColor, light_direction, view_direction, normal_worldspace, pointLight.color);
  } else if (lighting.lightType == 1) {
    var directionalLight: DirectionalLight = lighting_getDirectionalLight(0);
    lightColor += lighting_getLightColor(surfaceColor, -directionalLight.direction, view_direction, normal_worldspace, directionalLight.color);
  }
  
  return lightColor;
  /*
  for (int i = 0; i < MAX_LIGHTS; i++) {
    if (i >= lighting.pointLightCount) {
      break;
    }
    PointLight pointLight = lighting.pointLight[i];
    vec3 light_position_worldspace = pointLight.position;
    vec3 light_direction = normalize(light_position_worldspace - position_worldspace);
    lightColor += lighting_getLightColor(surfaceColor, light_direction, view_direction, normal_worldspace, pointLight.color);
  }

  for (int i = 0; i < MAX_LIGHTS; i++) {
    if (i >= lighting.directionalLightCount) {
      break;
    }
    DirectionalLight directionalLight = lighting.directionalLight[i];
    lightColor += lighting_getLightColor(surfaceColor, -directionalLight.direction, view_direction, normal_worldspace, directionalLight.color);
  }
  */
}

fn lighting_getSpecularLightColor(cameraPosition: vec3<f32>, position_worldspace: vec3<f32>, normal_worldspace: vec3<f32>) -> vec3<f32>{
  var lightColor = vec3<f32>(0, 0, 0);
  let surfaceColor = vec3<f32>(0, 0, 0);

  if (lighting.enabled == 0) {
    let view_direction = normalize(cameraPosition - position_worldspace);

    switch (lighting.lightType) {
      case 0, default: {
        let pointLight: PointLight = lighting_getPointLight(0);
        let light_position_worldspace: vec3<f32> = pointLight.position;
        let light_direction: vec3<f32> = normalize(light_position_worldspace - position_worldspace);
        lightColor += lighting_getLightColor(surfaceColor, light_direction, view_direction, normal_worldspace, pointLight.color);
      }
      case 1: {
        let directionalLight: DirectionalLight = lighting_getDirectionalLight(0);
        lightColor += lighting_getLightColor(surfaceColor, -directionalLight.direction, view_direction, normal_worldspace, directionalLight.color);
      }
    }
  }
  return lightColor;
}
`;var _n={props:{},name:"gouraudMaterial",vs:Ft.replace("phongMaterial","gouraudMaterial"),fs:Pt.replace("phongMaterial","gouraudMaterial"),source:Dt.replaceAll("phongMaterial","gouraudMaterial"),defines:{LIGHTING_VERTEX:!0},dependencies:[te],uniformTypes:{ambient:"f32",diffuse:"f32",shininess:"f32",specularColor:"vec3<f32>"},defaultUniforms:{ambient:.35,diffuse:.6,shininess:32,specularColor:[.15,.15,.15]},getUniforms(a){let e={...a};return e.specularColor&&(e.specularColor=e.specularColor.map(t=>t/255)),{..._n.defaultUniforms,...e}}};var gn={name:"phongMaterial",dependencies:[te],source:Dt,vs:Pt,fs:Ft,defines:{LIGHTING_FRAGMENT:!0},uniformTypes:{ambient:"f32",diffuse:"f32",shininess:"f32",specularColor:"vec3<f32>"},defaultUniforms:{ambient:.35,diffuse:.6,shininess:32,specularColor:[.15,.15,.15]},getUniforms(a){let e={...a};return e.specularColor&&(e.specularColor=e.specularColor.map(t=>t/255)),{...gn.defaultUniforms,...e}}};var mr=`out vec3 pbr_vPosition;
out vec2 pbr_vUV;

#ifdef HAS_NORMALS
# ifdef HAS_TANGENTS
out mat3 pbr_vTBN;
# else
out vec3 pbr_vNormal;
# endif
#endif

void pbr_setPositionNormalTangentUV(vec4 position, vec4 normal, vec4 tangent, vec2 uv)
{
  vec4 pos = pbrProjection.modelMatrix * position;
  pbr_vPosition = vec3(pos.xyz) / pos.w;

#ifdef HAS_NORMALS
#ifdef HAS_TANGENTS
  vec3 normalW = normalize(vec3(pbrProjection.normalMatrix * vec4(normal.xyz, 0.0)));
  vec3 tangentW = normalize(vec3(pbrProjection.modelMatrix * vec4(tangent.xyz, 0.0)));
  vec3 bitangentW = cross(normalW, tangentW) * tangent.w;
  pbr_vTBN = mat3(tangentW, bitangentW, normalW);
#else // HAS_TANGENTS != 1
  pbr_vNormal = normalize(vec3(pbrProjection.modelMatrix * vec4(normal.xyz, 0.0)));
#endif
#endif

#ifdef HAS_UV
  pbr_vUV = uv;
#else
  pbr_vUV = vec2(0.,0.);
#endif
}
`,_r=`precision highp float;

uniform pbrMaterialUniforms {
  // Material is unlit
  bool unlit;

  // Base color map
  bool baseColorMapEnabled;
  vec4 baseColorFactor;

  bool normalMapEnabled;  
  float normalScale; // #ifdef HAS_NORMALMAP

  bool emissiveMapEnabled;
  vec3 emissiveFactor; // #ifdef HAS_EMISSIVEMAP

  vec2 metallicRoughnessValues;
  bool metallicRoughnessMapEnabled;

  bool occlusionMapEnabled;
  float occlusionStrength; // #ifdef HAS_OCCLUSIONMAP
  
  bool alphaCutoffEnabled;
  float alphaCutoff; // #ifdef ALPHA_CUTOFF
  
  // IBL
  bool IBLenabled;
  vec2 scaleIBLAmbient; // #ifdef USE_IBL
  
  // debugging flags used for shader output of intermediate PBR variables
  // #ifdef PBR_DEBUG
  vec4 scaleDiffBaseMR;
  vec4 scaleFGDSpec;
  // #endif
} pbrMaterial;

// Samplers
#ifdef HAS_BASECOLORMAP
uniform sampler2D pbr_baseColorSampler;
#endif
#ifdef HAS_NORMALMAP
uniform sampler2D pbr_normalSampler;
#endif
#ifdef HAS_EMISSIVEMAP
uniform sampler2D pbr_emissiveSampler;
#endif
#ifdef HAS_METALROUGHNESSMAP
uniform sampler2D pbr_metallicRoughnessSampler;
#endif
#ifdef HAS_OCCLUSIONMAP
uniform sampler2D pbr_occlusionSampler;
#endif
#ifdef USE_IBL
uniform samplerCube pbr_diffuseEnvSampler;
uniform samplerCube pbr_specularEnvSampler;
uniform sampler2D pbr_brdfLUT;
#endif

// Inputs from vertex shader

in vec3 pbr_vPosition;
in vec2 pbr_vUV;

#ifdef HAS_NORMALS
#ifdef HAS_TANGENTS
in mat3 pbr_vTBN;
#else
in vec3 pbr_vNormal;
#endif
#endif

// Encapsulate the various inputs used by the various functions in the shading equation
// We store values in this struct to simplify the integration of alternative implementations
// of the shading terms, outlined in the Readme.MD Appendix.
struct PBRInfo {
  float NdotL;                  // cos angle between normal and light direction
  float NdotV;                  // cos angle between normal and view direction
  float NdotH;                  // cos angle between normal and half vector
  float LdotH;                  // cos angle between light direction and half vector
  float VdotH;                  // cos angle between view direction and half vector
  float perceptualRoughness;    // roughness value, as authored by the model creator (input to shader)
  float metalness;              // metallic value at the surface
  vec3 reflectance0;            // full reflectance color (normal incidence angle)
  vec3 reflectance90;           // reflectance color at grazing angle
  float alphaRoughness;         // roughness mapped to a more linear change in the roughness (proposed by [2])
  vec3 diffuseColor;            // color contribution from diffuse lighting
  vec3 specularColor;           // color contribution from specular lighting
  vec3 n;                       // normal at surface point
  vec3 v;                       // vector from surface point to camera
};

const float M_PI = 3.141592653589793;
const float c_MinRoughness = 0.04;

vec4 SRGBtoLINEAR(vec4 srgbIn)
{
#ifdef MANUAL_SRGB
#ifdef SRGB_FAST_APPROXIMATION
  vec3 linOut = pow(srgbIn.xyz,vec3(2.2));
#else // SRGB_FAST_APPROXIMATION
  vec3 bLess = step(vec3(0.04045),srgbIn.xyz);
  vec3 linOut = mix( srgbIn.xyz/vec3(12.92), pow((srgbIn.xyz+vec3(0.055))/vec3(1.055),vec3(2.4)), bLess );
#endif //SRGB_FAST_APPROXIMATION
  return vec4(linOut,srgbIn.w);;
#else //MANUAL_SRGB
  return srgbIn;
#endif //MANUAL_SRGB
}

// Find the normal for this fragment, pulling either from a predefined normal map
// or from the interpolated mesh normal and tangent attributes.
vec3 getNormal()
{
  // Retrieve the tangent space matrix
#ifndef HAS_TANGENTS
  vec3 pos_dx = dFdx(pbr_vPosition);
  vec3 pos_dy = dFdy(pbr_vPosition);
  vec3 tex_dx = dFdx(vec3(pbr_vUV, 0.0));
  vec3 tex_dy = dFdy(vec3(pbr_vUV, 0.0));
  vec3 t = (tex_dy.t * pos_dx - tex_dx.t * pos_dy) / (tex_dx.s * tex_dy.t - tex_dy.s * tex_dx.t);

#ifdef HAS_NORMALS
  vec3 ng = normalize(pbr_vNormal);
#else
  vec3 ng = cross(pos_dx, pos_dy);
#endif

  t = normalize(t - ng * dot(ng, t));
  vec3 b = normalize(cross(ng, t));
  mat3 tbn = mat3(t, b, ng);
#else // HAS_TANGENTS
  mat3 tbn = pbr_vTBN;
#endif

#ifdef HAS_NORMALMAP
  vec3 n = texture(pbr_normalSampler, pbr_vUV).rgb;
  n = normalize(tbn * ((2.0 * n - 1.0) * vec3(pbrMaterial.normalScale, pbrMaterial.normalScale, 1.0)));
#else
  // The tbn matrix is linearly interpolated, so we need to re-normalize
  vec3 n = normalize(tbn[2].xyz);
#endif

  return n;
}

// Calculation of the lighting contribution from an optional Image Based Light source.
// Precomputed Environment Maps are required uniform inputs and are computed as outlined in [1].
// See our README.md on Environment Maps [3] for additional discussion.
#ifdef USE_IBL
vec3 getIBLContribution(PBRInfo pbrInfo, vec3 n, vec3 reflection)
{
  float mipCount = 9.0; // resolution of 512x512
  float lod = (pbrInfo.perceptualRoughness * mipCount);
  // retrieve a scale and bias to F0. See [1], Figure 3
  vec3 brdf = SRGBtoLINEAR(texture(pbr_brdfLUT,
    vec2(pbrInfo.NdotV, 1.0 - pbrInfo.perceptualRoughness))).rgb;
  vec3 diffuseLight = SRGBtoLINEAR(texture(pbr_diffuseEnvSampler, n)).rgb;

#ifdef USE_TEX_LOD
  vec3 specularLight = SRGBtoLINEAR(texture(pbr_specularEnvSampler, reflection, lod)).rgb;
#else
  vec3 specularLight = SRGBtoLINEAR(texture(pbr_specularEnvSampler, reflection)).rgb;
#endif

  vec3 diffuse = diffuseLight * pbrInfo.diffuseColor;
  vec3 specular = specularLight * (pbrInfo.specularColor * brdf.x + brdf.y);

  // For presentation, this allows us to disable IBL terms
  diffuse *= pbrMaterial.scaleIBLAmbient.x;
  specular *= pbrMaterial.scaleIBLAmbient.y;

  return diffuse + specular;
}
#endif

// Basic Lambertian diffuse
// Implementation from Lambert's Photometria https://archive.org/details/lambertsphotome00lambgoog
// See also [1], Equation 1
vec3 diffuse(PBRInfo pbrInfo)
{
  return pbrInfo.diffuseColor / M_PI;
}

// The following equation models the Fresnel reflectance term of the spec equation (aka F())
// Implementation of fresnel from [4], Equation 15
vec3 specularReflection(PBRInfo pbrInfo)
{
  return pbrInfo.reflectance0 +
    (pbrInfo.reflectance90 - pbrInfo.reflectance0) *
    pow(clamp(1.0 - pbrInfo.VdotH, 0.0, 1.0), 5.0);
}

// This calculates the specular geometric attenuation (aka G()),
// where rougher material will reflect less light back to the viewer.
// This implementation is based on [1] Equation 4, and we adopt their modifications to
// alphaRoughness as input as originally proposed in [2].
float geometricOcclusion(PBRInfo pbrInfo)
{
  float NdotL = pbrInfo.NdotL;
  float NdotV = pbrInfo.NdotV;
  float r = pbrInfo.alphaRoughness;

  float attenuationL = 2.0 * NdotL / (NdotL + sqrt(r * r + (1.0 - r * r) * (NdotL * NdotL)));
  float attenuationV = 2.0 * NdotV / (NdotV + sqrt(r * r + (1.0 - r * r) * (NdotV * NdotV)));
  return attenuationL * attenuationV;
}

// The following equation(s) model the distribution of microfacet normals across
// the area being drawn (aka D())
// Implementation from "Average Irregularity Representation of a Roughened Surface
// for Ray Reflection" by T. S. Trowbridge, and K. P. Reitz
// Follows the distribution function recommended in the SIGGRAPH 2013 course notes
// from EPIC Games [1], Equation 3.
float microfacetDistribution(PBRInfo pbrInfo)
{
  float roughnessSq = pbrInfo.alphaRoughness * pbrInfo.alphaRoughness;
  float f = (pbrInfo.NdotH * roughnessSq - pbrInfo.NdotH) * pbrInfo.NdotH + 1.0;
  return roughnessSq / (M_PI * f * f);
}

void PBRInfo_setAmbientLight(inout PBRInfo pbrInfo) {
  pbrInfo.NdotL = 1.0;
  pbrInfo.NdotH = 0.0;
  pbrInfo.LdotH = 0.0;
  pbrInfo.VdotH = 1.0;
}

void PBRInfo_setDirectionalLight(inout PBRInfo pbrInfo, vec3 lightDirection) {
  vec3 n = pbrInfo.n;
  vec3 v = pbrInfo.v;
  vec3 l = normalize(lightDirection);             // Vector from surface point to light
  vec3 h = normalize(l+v);                        // Half vector between both l and v

  pbrInfo.NdotL = clamp(dot(n, l), 0.001, 1.0);
  pbrInfo.NdotH = clamp(dot(n, h), 0.0, 1.0);
  pbrInfo.LdotH = clamp(dot(l, h), 0.0, 1.0);
  pbrInfo.VdotH = clamp(dot(v, h), 0.0, 1.0);
}

void PBRInfo_setPointLight(inout PBRInfo pbrInfo, PointLight pointLight) {
  vec3 light_direction = normalize(pointLight.position - pbr_vPosition);
  PBRInfo_setDirectionalLight(pbrInfo, light_direction);
}

vec3 calculateFinalColor(PBRInfo pbrInfo, vec3 lightColor) {
  // Calculate the shading terms for the microfacet specular shading model
  vec3 F = specularReflection(pbrInfo);
  float G = geometricOcclusion(pbrInfo);
  float D = microfacetDistribution(pbrInfo);

  // Calculation of analytical lighting contribution
  vec3 diffuseContrib = (1.0 - F) * diffuse(pbrInfo);
  vec3 specContrib = F * G * D / (4.0 * pbrInfo.NdotL * pbrInfo.NdotV);
  // Obtain final intensity as reflectance (BRDF) scaled by the energy of the light (cosine law)
  return pbrInfo.NdotL * lightColor * (diffuseContrib + specContrib);
}

vec4 pbr_filterColor(vec4 colorUnused)
{
  // The albedo may be defined from a base texture or a flat color
#ifdef HAS_BASECOLORMAP
  vec4 baseColor = SRGBtoLINEAR(texture(pbr_baseColorSampler, pbr_vUV)) * pbrMaterial.baseColorFactor;
#else
  vec4 baseColor = pbrMaterial.baseColorFactor;
#endif

#ifdef ALPHA_CUTOFF
  if (baseColor.a < pbrMaterial.alphaCutoff) {
    discard;
  }
#endif

  vec3 color = vec3(0, 0, 0);

  if(pbrMaterial.unlit){
    color.rgb = baseColor.rgb;
  }
  else{
    // Metallic and Roughness material properties are packed together
    // In glTF, these factors can be specified by fixed scalar values
    // or from a metallic-roughness map
    float perceptualRoughness = pbrMaterial.metallicRoughnessValues.y;
    float metallic = pbrMaterial.metallicRoughnessValues.x;
#ifdef HAS_METALROUGHNESSMAP
    // Roughness is stored in the 'g' channel, metallic is stored in the 'b' channel.
    // This layout intentionally reserves the 'r' channel for (optional) occlusion map data
    vec4 mrSample = texture(pbr_metallicRoughnessSampler, pbr_vUV);
    perceptualRoughness = mrSample.g * perceptualRoughness;
    metallic = mrSample.b * metallic;
#endif
    perceptualRoughness = clamp(perceptualRoughness, c_MinRoughness, 1.0);
    metallic = clamp(metallic, 0.0, 1.0);
    // Roughness is authored as perceptual roughness; as is convention,
    // convert to material roughness by squaring the perceptual roughness [2].
    float alphaRoughness = perceptualRoughness * perceptualRoughness;

    vec3 f0 = vec3(0.04);
    vec3 diffuseColor = baseColor.rgb * (vec3(1.0) - f0);
    diffuseColor *= 1.0 - metallic;
    vec3 specularColor = mix(f0, baseColor.rgb, metallic);

    // Compute reflectance.
    float reflectance = max(max(specularColor.r, specularColor.g), specularColor.b);

    // For typical incident reflectance range (between 4% to 100%) set the grazing
    // reflectance to 100% for typical fresnel effect.
    // For very low reflectance range on highly diffuse objects (below 4%),
    // incrementally reduce grazing reflecance to 0%.
    float reflectance90 = clamp(reflectance * 25.0, 0.0, 1.0);
    vec3 specularEnvironmentR0 = specularColor.rgb;
    vec3 specularEnvironmentR90 = vec3(1.0, 1.0, 1.0) * reflectance90;

    vec3 n = getNormal();                          // normal at surface point
    vec3 v = normalize(pbrProjection.camera - pbr_vPosition);  // Vector from surface point to camera

    float NdotV = clamp(abs(dot(n, v)), 0.001, 1.0);
    vec3 reflection = -normalize(reflect(v, n));

    PBRInfo pbrInfo = PBRInfo(
      0.0, // NdotL
      NdotV,
      0.0, // NdotH
      0.0, // LdotH
      0.0, // VdotH
      perceptualRoughness,
      metallic,
      specularEnvironmentR0,
      specularEnvironmentR90,
      alphaRoughness,
      diffuseColor,
      specularColor,
      n,
      v
    );


#ifdef USE_LIGHTS
    // Apply ambient light
    PBRInfo_setAmbientLight(pbrInfo);
    color += calculateFinalColor(pbrInfo, lighting.ambientColor);

    // Apply directional light
    for(int i = 0; i < lighting.directionalLightCount; i++) {
      if (i < lighting.directionalLightCount) {
        PBRInfo_setDirectionalLight(pbrInfo, lighting_getDirectionalLight(i).direction);
        color += calculateFinalColor(pbrInfo, lighting_getDirectionalLight(i).color);
      }
    }

    // Apply point light
    for(int i = 0; i < lighting.pointLightCount; i++) {
      if (i < lighting.pointLightCount) {
        PBRInfo_setPointLight(pbrInfo, lighting_getPointLight(i));
        float attenuation = getPointLightAttenuation(lighting_getPointLight(i), distance(lighting_getPointLight(i).position, pbr_vPosition));
        color += calculateFinalColor(pbrInfo, lighting_getPointLight(i).color / attenuation);
      }
    }
#endif

    // Calculate lighting contribution from image based lighting source (IBL)
#ifdef USE_IBL
    if (pbrMaterial.IBLenabled) {
      color += getIBLContribution(pbrInfo, n, reflection);
    }
#endif

 // Apply optional PBR terms for additional (optional) shading
#ifdef HAS_OCCLUSIONMAP
    if (pbrMaterial.occlusionMapEnabled) {
      float ao = texture(pbr_occlusionSampler, pbr_vUV).r;
      color = mix(color, color * ao, pbrMaterial.occlusionStrength);
    }
#endif

#ifdef HAS_EMISSIVEMAP
    if (pbrMaterial.emissiveMapEnabled) {
      vec3 emissive = SRGBtoLINEAR(texture(pbr_emissiveSampler, pbr_vUV)).rgb * pbrMaterial.emissiveFactor;
      color += emissive;
    }
#endif

    // This section uses mix to override final color for reference app visualization
    // of various parameters in the lighting equation.
#ifdef PBR_DEBUG
    // TODO: Figure out how to debug multiple lights

    // color = mix(color, F, pbr_scaleFGDSpec.x);
    // color = mix(color, vec3(G), pbr_scaleFGDSpec.y);
    // color = mix(color, vec3(D), pbr_scaleFGDSpec.z);
    // color = mix(color, specContrib, pbr_scaleFGDSpec.w);

    // color = mix(color, diffuseContrib, pbr_scaleDiffBaseMR.x);
    color = mix(color, baseColor.rgb, pbrMaterial.scaleDiffBaseMR.y);
    color = mix(color, vec3(metallic), pbrMaterial.scaleDiffBaseMR.z);
    color = mix(color, vec3(perceptualRoughness), pbrMaterial.scaleDiffBaseMR.w);
#endif

  }

  return vec4(pow(color,vec3(1.0/2.2)), baseColor.a);
}
`;var gr=`struct PBRFragmentInputs {
  pbr_vPosition: vec3f,
  pbr_vUV: vec2f,
  pbr_vTBN: mat3f,
  pbr_vNormal: vec3f
};

var fragmentInputs: PBRFragmentInputs;

fn pbr_setPositionNormalTangentUV(position: vec4f, normal: vec4f, tangent: vec4f, uv: vec2f)
{
  var pos: vec4f = pbrProjection.modelMatrix * position;
  pbr_vPosition = vec3(pos.xyz) / pos.w;

#ifdef HAS_NORMALS
#ifdef HAS_TANGENTS
  let normalW: vec3f = normalize(vec3(pbrProjection.normalMatrix * vec4(normal.xyz, 0.0)));
  let tangentW: vec3f = normalize(vec3(pbrProjection.modelMatrix * vec4(tangent.xyz, 0.0)));
  let bitangentW: vec3f = cross(normalW, tangentW) * tangent.w;
  fragmentInputs,pbr_vTBN = mat3(tangentW, bitangentW, normalW);
#else // HAS_TANGENTS != 1
  fragmentInputs.pbr_vNormal = normalize(vec3(pbrProjection.modelMatrix * vec4(normal.xyz, 0.0)));
#endif
#endif

#ifdef HAS_UV
  pbr_vUV = uv;
#else
  pbr_vUV = vec2(0.,0.);
#endif
}

struct pbrMaterialUniforms {
  // Material is unlit
  unlit: uint32,

  // Base color map
  baseColorMapEnabled: uint32,
  baseColorFactor: vec4f,

  normalMapEnabled : uint32,
  normalScale: f32,  // #ifdef HAS_NORMALMAP

  emissiveMapEnabled: uint32,
  emissiveFactor: vec3f, // #ifdef HAS_EMISSIVEMAP

  metallicRoughnessValues: vec2f,
  metallicRoughnessMapEnabled: uint32,

  occlusionMapEnabled: i32,
  occlusionStrength: f32, // #ifdef HAS_OCCLUSIONMAP
  
  alphaCutoffEnabled: i32,
  alphaCutoff: f32, // #ifdef ALPHA_CUTOFF
  
  // IBL
  IBLenabled: i32,
  scaleIBLAmbient: vec2f, // #ifdef USE_IBL
  
  // debugging flags used for shader output of intermediate PBR variables
  // #ifdef PBR_DEBUG
  scaleDiffBaseMR: vec4f,
  scaleFGDSpec: vec4f
  // #endif
} 
  
@binding(2) @group(0) var<uniform> material : pbrMaterialUniforms;

// Samplers
#ifdef HAS_BASECOLORMAP
uniform sampler2D pbr_baseColorSampler;
#endif
#ifdef HAS_NORMALMAP
uniform sampler2D pbr_normalSampler;
#endif
#ifdef HAS_EMISSIVEMAP
uniform sampler2D pbr_emissiveSampler;
#endif
#ifdef HAS_METALROUGHNESSMAP
uniform sampler2D pbr_metallicRoughnessSampler;
#endif
#ifdef HAS_OCCLUSIONMAP
uniform sampler2D pbr_occlusionSampler;
#endif
#ifdef USE_IBL
uniform samplerCube pbr_diffuseEnvSampler;
uniform samplerCube pbr_specularEnvSampler;
uniform sampler2D pbr_brdfLUT;
#endif

// Encapsulate the various inputs used by the various functions in the shading equation
// We store values in this struct to simplify the integration of alternative implementations
// of the shading terms, outlined in the Readme.MD Appendix.
struct PBRInfo {
  NdotL: f32,                  // cos angle between normal and light direction
  NdotV: f32,                  // cos angle between normal and view direction
  NdotH: f32,                  // cos angle between normal and half vector
  LdotH: f32,                  // cos angle between light direction and half vector
  VdotH: f32,                  // cos angle between view direction and half vector
  perceptualRoughness: f32,    // roughness value, as authored by the model creator (input to shader)
  metalness: f32,              // metallic value at the surface
  reflectance0: vec3f,            // full reflectance color (normal incidence angle)
  reflectance90: vec3f,           // reflectance color at grazing angle
  alphaRoughness: f32,         // roughness mapped to a more linear change in the roughness (proposed by [2])
  diffuseColor: vec3f,            // color contribution from diffuse lighting
  specularColor: vec3f,           // color contribution from specular lighting
  n: vec3f,                       // normal at surface point
  v: vec3f,                       // vector from surface point to camera
};

const M_PI = 3.141592653589793;
const c_MinRoughness = 0.04;

fn SRGBtoLINEAR(srgbIn: vec4f ) -> vec4f
{
#ifdef MANUAL_SRGB
#ifdef SRGB_FAST_APPROXIMATION
  var linOut: vec3f = pow(srgbIn.xyz,vec3(2.2));
#else // SRGB_FAST_APPROXIMATION
  var bLess: vec3f = step(vec3(0.04045),srgbIn.xyz);
  var linOut: vec3f = mix( srgbIn.xyz/vec3(12.92), pow((srgbIn.xyz+vec3(0.055))/vec3(1.055),vec3(2.4)), bLess );
#endif //SRGB_FAST_APPROXIMATION
  return vec4f(linOut,srgbIn.w);;
#else //MANUAL_SRGB
  return srgbIn;
#endif //MANUAL_SRGB
}

// Find the normal for this fragment, pulling either from a predefined normal map
// or from the interpolated mesh normal and tangent attributes.
fn getNormal() -> vec3f
{
  // Retrieve the tangent space matrix
#ifndef HAS_TANGENTS
  var pos_dx: vec3f = dFdx(pbr_vPosition);
  var pos_dy: vec3f = dFdy(pbr_vPosition);
  var tex_dx: vec3f = dFdx(vec3(pbr_vUV, 0.0));
  var tex_dy: vec3f = dFdy(vec3(pbr_vUV, 0.0));
  var t: vec3f = (tex_dy.t * pos_dx - tex_dx.t * pos_dy) / (tex_dx.s * tex_dy.t - tex_dy.s * tex_dx.t);

#ifdef HAS_NORMALS
  var ng: vec3f = normalize(pbr_vNormal);
#else
  var ng: vec3f = cross(pos_dx, pos_dy);
#endif

  t = normalize(t - ng * dot(ng, t));
  var b: vec3f = normalize(cross(ng, t));
  var tbn: mat3f = mat3f(t, b, ng);
#else // HAS_TANGENTS
  var tbn: mat3f = pbr_vTBN;
#endif

#ifdef HAS_NORMALMAP
  vec3 n = texture(pbr_normalSampler, pbr_vUV).rgb;
  n = normalize(tbn * ((2.0 * n - 1.0) * vec3(pbrMaterial.normalScale, pbrMaterial.normalScale, 1.0)));
#else
  // The tbn matrix is linearly interpolated, so we need to re-normalize
  vec3 n = normalize(tbn[2].xyz);
#endif

  return n;
}

// Calculation of the lighting contribution from an optional Image Based Light source.
// Precomputed Environment Maps are required uniform inputs and are computed as outlined in [1].
// See our README.md on Environment Maps [3] for additional discussion.
#ifdef USE_IBL
fn getIBLContribution(PBRInfo pbrInfo, vec3 n, vec3 reflection) -> vec3f
{
  float mipCount = 9.0; // resolution of 512x512
  float lod = (pbrInfo.perceptualRoughness * mipCount);
  // retrieve a scale and bias to F0. See [1], Figure 3
  vec3 brdf = SRGBtoLINEAR(texture(pbr_brdfLUT,
    vec2(pbrInfo.NdotV, 1.0 - pbrInfo.perceptualRoughness))).rgb;
  vec3 diffuseLight = SRGBtoLINEAR(texture(pbr_diffuseEnvSampler, n)).rgb;

#ifdef USE_TEX_LOD
  vec3 specularLight = SRGBtoLINEAR(texture(pbr_specularEnvSampler, reflection, lod)).rgb;
#else
  vec3 specularLight = SRGBtoLINEAR(texture(pbr_specularEnvSampler, reflection)).rgb;
#endif

  vec3 diffuse = diffuseLight * pbrInfo.diffuseColor;
  vec3 specular = specularLight * (pbrInfo.specularColor * brdf.x + brdf.y);

  // For presentation, this allows us to disable IBL terms
  diffuse *= pbrMaterial.scaleIBLAmbient.x;
  specular *= pbrMaterial.scaleIBLAmbient.y;

  return diffuse + specular;
}
#endif

// Basic Lambertian diffuse
// Implementation from Lambert's Photometria https://archive.org/details/lambertsphotome00lambgoog
// See also [1], Equation 1
fn diffuse(pbrInfo: PBRInfo) -> vec3<f32> {
  return pbrInfo.diffuseColor / PI;
}

// The following equation models the Fresnel reflectance term of the spec equation (aka F())
// Implementation of fresnel from [4], Equation 15
fn specularReflection(pbrInfo: PBRInfo) -> vec3<f32> {
  return pbrInfo.reflectance0 +
    (pbrInfo.reflectance90 - pbrInfo.reflectance0) *
    pow(clamp(1.0 - pbrInfo.VdotH, 0.0, 1.0), 5.0);
}

// This calculates the specular geometric attenuation (aka G()),
// where rougher material will reflect less light back to the viewer.
// This implementation is based on [1] Equation 4, and we adopt their modifications to
// alphaRoughness as input as originally proposed in [2].
fn geometricOcclusion(pbrInfo: PBRInfo) -> f32 {
  let NdotL: f32 = pbrInfo.NdotL;
  let NdotV: f32 = pbrInfo.NdotV;
  let r: f32 = pbrInfo.alphaRoughness;

  let attenuationL = 2.0 * NdotL / (NdotL + sqrt(r * r + (1.0 - r * r) * (NdotL * NdotL)));
  let attenuationV = 2.0 * NdotV / (NdotV + sqrt(r * r + (1.0 - r * r) * (NdotV * NdotV)));
  return attenuationL * attenuationV;
}

// The following equation(s) model the distribution of microfacet normals across
// the area being drawn (aka D())
// Implementation from "Average Irregularity Representation of a Roughened Surface
// for Ray Reflection" by T. S. Trowbridge, and K. P. Reitz
// Follows the distribution function recommended in the SIGGRAPH 2013 course notes
// from EPIC Games [1], Equation 3.
fn microfacetDistribution(pbrInfo: PBRInfo) -> f32 {
  let roughnessSq = pbrInfo.alphaRoughness * pbrInfo.alphaRoughness;
  let f = (pbrInfo.NdotH * roughnessSq - pbrInfo.NdotH) * pbrInfo.NdotH + 1.0;
  return roughnessSq / (PI * f * f);
}

fn PBRInfo_setAmbientLight(pbrInfo: ptr<function, PBRInfo>) {
  (*pbrInfo).NdotL = 1.0;
  (*pbrInfo).NdotH = 0.0;
  (*pbrInfo).LdotH = 0.0;
  (*pbrInfo).VdotH = 1.0;
}

fn PBRInfo_setDirectionalLight(pbrInfo: ptr<function, PBRInfo>, lightDirection: vec3<f32>) {
  let n = (*pbrInfo).n;
  let v = (*pbrInfo).v;
  let l = normalize(lightDirection);             // Vector from surface point to light
  let h = normalize(l + v);                      // Half vector between both l and v

  (*pbrInfo).NdotL = clamp(dot(n, l), 0.001, 1.0);
  (*pbrInfo).NdotH = clamp(dot(n, h), 0.0, 1.0);
  (*pbrInfo).LdotH = clamp(dot(l, h), 0.0, 1.0);
  (*pbrInfo).VdotH = clamp(dot(v, h), 0.0, 1.0);
}

fn PBRInfo_setPointLight(pbrInfo: ptr<function, PBRInfo>, pointLight: PointLight) {
  let light_direction = normalize(pointLight.position - pbr_vPosition);
  PBRInfo_setDirectionalLight(pbrInfo, light_direction);
}

fn calculateFinalColor(pbrInfo: PBRInfo, lightColor: vec3<f32>) -> vec3<f32> {
  // Calculate the shading terms for the microfacet specular shading model
  let F = specularReflection(pbrInfo);
  let G = geometricOcclusion(pbrInfo);
  let D = microfacetDistribution(pbrInfo);

  // Calculation of analytical lighting contribution
  let diffuseContrib = (1.0 - F) * diffuse(pbrInfo);
  let specContrib = F * G * D / (4.0 * pbrInfo.NdotL * pbrInfo.NdotV);
  // Obtain final intensity as reflectance (BRDF) scaled by the energy of the light (cosine law)
  return pbrInfo.NdotL * lightColor * (diffuseContrib + specContrib);
}

fn pbr_filterColor(colorUnused: vec4<f32>) -> vec4<f32> {
  // The albedo may be defined from a base texture or a flat color
  var baseColor: vec4<f32>;
  #ifdef HAS_BASECOLORMAP
  baseColor = SRGBtoLINEAR(textureSample(pbr_baseColorSampler, pbr_baseColorSampler, pbr_vUV)) * pbrMaterial.baseColorFactor;
  #else
  baseColor = pbrMaterial.baseColorFactor;
  #endif

  #ifdef ALPHA_CUTOFF
  if (baseColor.a < pbrMaterial.alphaCutoff) {
    discard;
  }
  #endif

  var color = vec3<f32>(0.0, 0.0, 0.0);

  if (pbrMaterial.unlit) {
    color = baseColor.rgb;
  } else {
    // Metallic and Roughness material properties are packed together
    // In glTF, these factors can be specified by fixed scalar values
    // or from a metallic-roughness map
    var perceptualRoughness = pbrMaterial.metallicRoughnessValues.y;
    var metallic = pbrMaterial.metallicRoughnessValues.x;
    #ifdef HAS_METALROUGHNESSMAP
    // Roughness is stored in the 'g' channel, metallic is stored in the 'b' channel.
    // This layout intentionally reserves the 'r' channel for (optional) occlusion map data
    let mrSample = textureSample(pbr_metallicRoughnessSampler, pbr_metallicRoughnessSampler, pbr_vUV);
    perceptualRoughness = mrSample.g * perceptualRoughness;
    metallic = mrSample.b * metallic;
    #endif
    perceptualRoughness = clamp(perceptualRoughness, c_MinRoughness, 1.0);
    metallic = clamp(metallic, 0.0, 1.0);
    // Roughness is authored as perceptual roughness; as is convention,
    // convert to material roughness by squaring the perceptual roughness [2].
    let alphaRoughness = perceptualRoughness * perceptualRoughness;

    let f0 = vec3<f32>(0.04);
    var diffuseColor = baseColor.rgb * (vec3<f32>(1.0) - f0);
    diffuseColor *= 1.0 - metallic;
    let specularColor = mix(f0, baseColor.rgb, metallic);

    // Compute reflectance.
    let reflectance = max(max(specularColor.r, specularColor.g), specularColor.b);

    // For typical incident reflectance range (between 4% to 100%) set the grazing
    // reflectance to 100% for typical fresnel effect.
    // For very low reflectance range on highly diffuse objects (below 4%),
    // incrementally reduce grazing reflectance to 0%.
    let reflectance90 = clamp(reflectance * 25.0, 0.0, 1.0);
    let specularEnvironmentR0 = specularColor;
    let specularEnvironmentR90 = vec3<f32>(1.0, 1.0, 1.0) * reflectance90;

    let n = getNormal();                          // normal at surface point
    let v = normalize(pbrProjection.camera - pbr_vPosition);  // Vector from surface point to camera

    let NdotV = clamp(abs(dot(n, v)), 0.001, 1.0);
    let reflection = -normalize(reflect(v, n));

    var pbrInfo = PBRInfo(
      0.0, // NdotL
      NdotV,
      0.0, // NdotH
      0.0, // LdotH
      0.0, // VdotH
      perceptualRoughness,
      metallic,
      specularEnvironmentR0,
      specularEnvironmentR90,
      alphaRoughness,
      diffuseColor,
      specularColor,
      n,
      v
    );

    #ifdef USE_LIGHTS
    // Apply ambient light
    PBRInfo_setAmbientLight(&pbrInfo);
    color += calculateFinalColor(pbrInfo, lighting.ambientColor);

    // Apply directional light
    for (var i = 0; i < lighting.directionalLightCount; i++) {
      if (i < lighting.directionalLightCount) {
        PBRInfo_setDirectionalLight(&pbrInfo, lighting_getDirectionalLight(i).direction);
        color += calculateFinalColor(pbrInfo, lighting_getDirectionalLight(i).color);
      }
    }

    // Apply point light
    for (var i = 0; i < lighting.pointLightCount; i++) {
      if (i < lighting.pointLightCount) {
        PBRInfo_setPointLight(&pbrInfo, lighting_getPointLight(i));
        let attenuation = getPointLightAttenuation(lighting_getPointLight(i), distance(lighting_getPointLight(i).position, pbr_vPosition));
        color += calculateFinalColor(pbrInfo, lighting_getPointLight(i).color / attenuation);
      }
    }
    #endif

    // Calculate lighting contribution from image based lighting source (IBL)
    #ifdef USE_IBL
    if (pbrMaterial.IBLenabled) {
      color += getIBLContribution(pbrInfo, n, reflection);
    }
    #endif

    // Apply optional PBR terms for additional (optional) shading
    #ifdef HAS_OCCLUSIONMAP
    if (pbrMaterial.occlusionMapEnabled) {
      let ao = textureSample(pbr_occlusionSampler, pbr_occlusionSampler, pbr_vUV).r;
      color = mix(color, color * ao, pbrMaterial.occlusionStrength);
    }
    #endif

    #ifdef HAS_EMISSIVEMAP
    if (pbrMaterial.emissiveMapEnabled) {
      let emissive = SRGBtoLINEAR(textureSample(pbr_emissiveSampler, pbr_emissiveSampler, pbr_vUV)).rgb * pbrMaterial.emissiveFactor;
      color += emissive;
    }
    #endif

    // This section uses mix to override final color for reference app visualization
    // of various parameters in the lighting equation.
    #ifdef PBR_DEBUG
    // TODO: Figure out how to debug multiple lights

    // color = mix(color, F, pbr_scaleFGDSpec.x);
    // color = mix(color, vec3(G), pbr_scaleFGDSpec.y);
    // color = mix(color, vec3(D), pbr_scaleFGDSpec.z);
    // color = mix(color, specContrib, pbr_scaleFGDSpec.w);

    // color = mix(color, diffuseContrib, pbr_scaleDiffBaseMR.x);
    color = mix(color, baseColor.rgb, pbrMaterial.scaleDiffBaseMR.y);
    color = mix(color, vec3<f32>(metallic), pbrMaterial.scaleDiffBaseMR.z);
    color = mix(color, vec3<f32>(perceptualRoughness), pbrMaterial.scaleDiffBaseMR.w);
    #endif
  }

  return vec4<f32>(pow(color, vec3<f32>(1.0 / 2.2)), baseColor.a);
}
`;var vr=`uniform pbrProjectionUniforms {
  mat4 modelViewProjectionMatrix;
  mat4 modelMatrix;
  mat4 normalMatrix;
  vec3 camera;
} pbrProjection;
`,br={name:"pbrProjection",vs:vr,fs:vr,getUniforms:a=>a,uniformTypes:{modelViewProjectionMatrix:"mat4x4<f32>",modelMatrix:"mat4x4<f32>",normalMatrix:"mat4x4<f32>",camera:"vec3<i32>"}};var xr={props:{},uniforms:{},name:"pbrMaterial",dependencies:[te,br],source:gr,vs:mr,fs:_r,defines:{LIGHTING_FRAGMENT:!0,HAS_NORMALMAP:!1,HAS_EMISSIVEMAP:!1,HAS_OCCLUSIONMAP:!1,HAS_BASECOLORMAP:!1,HAS_METALROUGHNESSMAP:!1,ALPHA_CUTOFF:!1,USE_IBL:!1,PBR_DEBUG:!1},getUniforms:a=>a,uniformTypes:{unlit:"i32",baseColorMapEnabled:"i32",baseColorFactor:"vec4<f32>",normalMapEnabled:"i32",normalScale:"f32",emissiveMapEnabled:"i32",emissiveFactor:"vec3<f32>",metallicRoughnessValues:"vec2<f32>",metallicRoughnessMapEnabled:"i32",occlusionMapEnabled:"i32",occlusionStrength:"f32",alphaCutoffEnabled:"i32",alphaCutoff:"f32",IBLenabled:"i32",scaleIBLAmbient:"vec2<f32>",scaleDiffBaseMR:"vec4<f32>",scaleFGDSpec:"vec4<f32>"}};return Er(Ze);})();
      return __exports__;
      });
