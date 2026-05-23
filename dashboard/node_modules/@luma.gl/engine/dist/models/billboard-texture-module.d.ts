export declare const billboardTexture: {
    readonly name: "billboardTexture";
    readonly fs: "#version 300 es\n\nuniform billboardTextureUniforms {\n  vec2 topLeft;\n  vec2 bottomRight;\n} billboardTexture;\n\nprecision highp float;\nuniform sampler2D backgroundTexture;\nout vec4 fragColor;\n\nvec2 billboardTexture_getTextureUV() {\n  ivec2 iTexSize = textureSize(backgroundTexture, 0) * 2;\n  vec2 texSize = vec2(float(iTexSize.x), float(iTexSize.y));\n  vec2 position = gl_FragCoord.xy / texSize;\n  return position;\n}\n\nvoid main(void) {\n  vec2 position = billboardTexture_getTextureUV();\n  fragColor = texture(backgroundTexture, position);\n}\n";
    readonly dependencies: [];
    readonly uniformTypes: {
        readonly topLeft: "vec2<f32>";
        readonly bottomRight: "vec2<f32>";
    };
};
//# sourceMappingURL=billboard-texture-module.d.ts.map