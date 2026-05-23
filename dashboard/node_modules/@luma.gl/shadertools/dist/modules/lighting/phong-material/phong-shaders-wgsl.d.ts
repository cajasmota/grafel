export declare const PHONG_WGSL = "struct phongMaterialUniforms {\n  ambient: f32,\n  diffuse: f32,\n  shininess: f32,\n  specularColor: vec3<f32>,\n};\n\n@binding(2) @group(0) var<uniform> phongMaterial : phongMaterialUniforms;\n\nfn lighting_getLightColor(surfaceColor: vec3<f32>, light_direction: vec3<f32>, view_direction: vec3<f32>, normal_worldspace: vec3<f32>, color: vec3<f32>) -> vec3<f32> {\n  let halfway_direction: vec3<f32> = normalize(light_direction + view_direction);\n  var lambertian: f32 = dot(light_direction, normal_worldspace);\n  var specular: f32 = 0.0;\n  if (lambertian > 0.0) {\n    let specular_angle = max(dot(normal_worldspace, halfway_direction), 0.0);\n    specular = pow(specular_angle, phongMaterial.shininess);\n  }\n  lambertian = max(lambertian, 0.0);\n  return (lambertian * phongMaterial.diffuse * surfaceColor + specular * phongMaterial.specularColor) * color;\n}\n\nfn lighting_getLightColor2(surfaceColor: vec3<f32>, cameraPosition: vec3<f32>, position_worldspace: vec3<f32>, normal_worldspace: vec3<f32>) -> vec3<f32> {\n  var lightColor: vec3<f32> = surfaceColor;\n\n  if (lighting.enabled == 0) {\n    return lightColor;\n  }\n\n  let view_direction: vec3<f32> = normalize(cameraPosition - position_worldspace);\n  lightColor = phongMaterial.ambient * surfaceColor * lighting.ambientColor;\n\n  if (lighting.lightType == 0) {\n    let pointLight: PointLight  = lighting_getPointLight(0);\n    let light_position_worldspace: vec3<f32> = pointLight.position;\n    let light_direction: vec3<f32> = normalize(light_position_worldspace - position_worldspace);\n    lightColor += lighting_getLightColor(surfaceColor, light_direction, view_direction, normal_worldspace, pointLight.color);\n  } else if (lighting.lightType == 1) {\n    var directionalLight: DirectionalLight = lighting_getDirectionalLight(0);\n    lightColor += lighting_getLightColor(surfaceColor, -directionalLight.direction, view_direction, normal_worldspace, directionalLight.color);\n  }\n  \n  return lightColor;\n  /*\n  for (int i = 0; i < MAX_LIGHTS; i++) {\n    if (i >= lighting.pointLightCount) {\n      break;\n    }\n    PointLight pointLight = lighting.pointLight[i];\n    vec3 light_position_worldspace = pointLight.position;\n    vec3 light_direction = normalize(light_position_worldspace - position_worldspace);\n    lightColor += lighting_getLightColor(surfaceColor, light_direction, view_direction, normal_worldspace, pointLight.color);\n  }\n\n  for (int i = 0; i < MAX_LIGHTS; i++) {\n    if (i >= lighting.directionalLightCount) {\n      break;\n    }\n    DirectionalLight directionalLight = lighting.directionalLight[i];\n    lightColor += lighting_getLightColor(surfaceColor, -directionalLight.direction, view_direction, normal_worldspace, directionalLight.color);\n  }\n  */\n}\n\nfn lighting_getSpecularLightColor(cameraPosition: vec3<f32>, position_worldspace: vec3<f32>, normal_worldspace: vec3<f32>) -> vec3<f32>{\n  var lightColor = vec3<f32>(0, 0, 0);\n  let surfaceColor = vec3<f32>(0, 0, 0);\n\n  if (lighting.enabled == 0) {\n    let view_direction = normalize(cameraPosition - position_worldspace);\n\n    switch (lighting.lightType) {\n      case 0, default: {\n        let pointLight: PointLight = lighting_getPointLight(0);\n        let light_position_worldspace: vec3<f32> = pointLight.position;\n        let light_direction: vec3<f32> = normalize(light_position_worldspace - position_worldspace);\n        lightColor += lighting_getLightColor(surfaceColor, light_direction, view_direction, normal_worldspace, pointLight.color);\n      }\n      case 1: {\n        let directionalLight: DirectionalLight = lighting_getDirectionalLight(0);\n        lightColor += lighting_getLightColor(surfaceColor, -directionalLight.direction, view_direction, normal_worldspace, directionalLight.color);\n      }\n    }\n  }\n  return lightColor;\n}\n";
/**
    for (int i = 0; i < MAX_LIGHTS; i++) {
      if (i >= lighting.pointLightCount) {
        break;
      }
      PointLight pointLight = lighting_getPointLight(i);
      vec3 light_position_worldspace = pointLight.position;
      vec3 light_direction = normalize(light_position_worldspace - position_worldspace);
      lightColor += lighting_getLightColor(surfaceColor, light_direction, view_direction, normal_worldspace, pointLight.color);
    }

    for (int i = 0; i < MAX_LIGHTS; i++) {
      if (i >= lighting.directionalLightCount) {
        break;
      }
      PointLight pointLight = lighting_getDirectionalLight(i);
      lightColor += lighting_getLightColor(surfaceColor, -directionalLight.direction, view_direction, normal_worldspace, directionalLight.color);
    }
  }
    /**
    for (int i = 0; i < MAX_LIGHTS; i++) {
      if (i >= lighting.pointLightCount) {
        break;
      }
      PointLight pointLight = lighting_getPointLight(i);
      vec3 light_position_worldspace = pointLight.position;
      vec3 light_direction = normalize(light_position_worldspace - position_worldspace);
      lightColor += lighting_getLightColor(surfaceColor, light_direction, view_direction, normal_worldspace, pointLight.color);
    }

    for (int i = 0; i < MAX_LIGHTS; i++) {
      if (i >= lighting.directionalLightCount) {
        break;
      }
      PointLight pointLight = lighting_getDirectionalLight(i);
      lightColor += lighting_getLightColor(surfaceColor, -directionalLight.direction, view_direction, normal_worldspace, directionalLight.color);
    }
  }
  */
//# sourceMappingURL=phong-shaders-wgsl.d.ts.map