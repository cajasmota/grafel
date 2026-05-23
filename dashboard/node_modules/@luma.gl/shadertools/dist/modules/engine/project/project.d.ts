import { Matrix4, Vector3 } from '@math.gl/core';
import type { NumberArray3, NumberArray16 } from '@math.gl/core';
/**
 * @note Projection uniforms are normally constant across draw calls,
 * at least for each view.
 */
export type ProjectionProps = {
    viewMatrix?: Readonly<Matrix4 | NumberArray16>;
    projectionMatrix?: Readonly<Matrix4 | NumberArray16>;
    cameraPositionWorld?: Readonly<Vector3 | NumberArray3>;
};
/**
 * @note Projection uniforms are normally constant across draw calls,
 * at least for each view.
 */
export type ProjectionUniforms = {
    viewMatrix?: Readonly<Matrix4 | NumberArray16>;
    projectionMatrix?: Readonly<Matrix4 | NumberArray16>;
    viewProjectionMatrix?: Readonly<Matrix4 | NumberArray16>;
    cameraPositionWorld?: Readonly<Vector3 | NumberArray3>;
};
declare function getUniforms(opts?: ProjectionProps, prevUniforms?: ProjectionUniforms): ProjectionUniforms;
/**
 * Projects coordinates
 */
export declare const projection: {
    readonly name: "projection";
    readonly uniformTypes: {
        readonly viewMatrix: "mat4x4<f32>";
        readonly projectionMatrix: "mat4x4<f32>";
        readonly viewProjectionMatrix: "mat4x4<f32>";
        readonly cameraPositionWorld: "vec3<f32>";
    };
    readonly getUniforms: typeof getUniforms;
    readonly vs: "varying vec4 project_vPositionWorld;\nvarying vec3 project_vNormalWorld;\n\n// Project uniform block\nuniform Project {\n  mat4 viewMatrix;\n  mat4 projectionMatrix;\n  mat4 viewProjectionMatrix;\n  vec3 cameraPositionWorld;\n} project;\n\nstruct World {\n  vec3 position;\n  vec3 normal;\n};\n\nWorld world;\n\nvoid project_setPosition(vec4 position) {\n  project_vPositionWorld = position;\n}\n\nvoid project_setNormal(vec3 normal) {\n  project_vNormalWorld = normal;\n}\n\nvoid project_setPositionAndNormal_World(vec3 position, vec3 normal) {\n  world.position = position;\n  world.normal = normal;\n}\n\nvoid project_setPositionAndNormal_Model(vec3 position, vec3 normal, mat4 modelMatrix) {\n  world.position = (modelMatrix * vec4(position, 1.)).xyz;\n  world.normal = mat3(modelMatrix) * normal;\n}\n\nvec4 project_model_to_clipspace(vec4 position) {\n  return project.viewProjectionMatrix * position;\n}\n\nvec4 project_model_to_clipspace_Model(vec3 position, mat4 modelMatrix) {\n  return project.viewProjectionMatrix * modelMatrix * vec4(position, 1.);\n}\n\nvec4 project_world_to_clipspace(vec3 position) {\n  return project.viewProjectionMatrix * vec4(position, 1.);\n}\n\nvec4 project_view_to_clipspace(vec3 position) {\n  return project.projectionMatrix * vec4(position, 1.);\n}\n\nvec4 project_to_clipspace(vec3 position) {\n  return project.viewProjectionMatrix * vec4(position, 1.);\n}\n";
    readonly fs: "varying vec4 project_vPositionWorld;\nvarying vec3 project_vNormalWorld;\n\nvec4 project_getPosition_World() {\n  return project_vPositionWorld;\n}\n\nvec3 project_getNormal_World() {\n  return project_vNormalWorld;\n}\n";
};
export {};
//# sourceMappingURL=project.d.ts.map