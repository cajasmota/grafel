/** Quick random generator for fragment shaders */
export declare const random: {
    readonly name: "random";
    readonly source: "fn random(scale: vec3f, seed: float) -> f32 {\n  /* use the fragment position for a different seed per-pixel */\n  return fract(sin(dot(gl_FragCoord.xyz + seed, scale)) * 43758.5453 + seed);\n}\n";
    readonly fs: "float random(vec3 scale, float seed) {\n  /* use the fragment position for a different seed per-pixel */\n  return fract(sin(dot(gl_FragCoord.xyz + seed, scale)) * 43758.5453 + seed);\n}\n";
};
//# sourceMappingURL=random.d.ts.map