// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { TruncatedConeGeometry } from "./truncated-cone-geometry.js";
import { uid } from "../utils/uid.js";
export class CylinderGeometry extends TruncatedConeGeometry {
    constructor(props = {}) {
        const { id = uid('cylinder-geometry'), radius = 1 } = props;
        super({
            ...props,
            id,
            bottomRadius: radius,
            topRadius: radius
        });
    }
}
//# sourceMappingURL=cylinder-geometry.js.map