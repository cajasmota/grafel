// Fixture for #2704 — template-literal variable resolution in HTTP paths.
//
// The leading interpolation of each call below is a bare identifier (`path`)
// whose binding is a same-file string-literal const. The extractor must
// substitute the literal value, producing `/things/{id}` rather than the
// pre-fix `{param}/{id}` / `{path}/{id}` placeholder shape.

import apiClient from "./client";

const path = "things";

export async function listThings() {
    return apiClient.get(`${path}/`);
}

export async function getThing(id) {
    return apiClient.get(`${path}/${id}`);
}

export async function updateThing(id, body) {
    return apiClient.patch(`${path}/${id}`, body);
}

export async function nestedThing(id, nestedId) {
    return apiClient.delete(`${path}/${id}/items/${nestedId}`);
}
