// Fixture for #2708 — arrow-fn template-literal factory inlining.
//
// `base(companyType, companyId)` is a same-file arrow function whose
// body is a template literal. The extractor must inline its body at the
// call site so that the synthetic http_endpoint_call entities use named
// `{companyType}` / `{companyId}` placeholders rather than the pre-fix
// `{param}` opacity.

import apiClient from "./client";

const base = (companyType, companyId) =>
    `/api/v1/${companyType}/${companyId}/branches`;

export const listBranches = async () =>
    apiClient.get(`${base(companyType, companyId)}/`);

export const getBranch = async (companyType, companyId, branchId) =>
    apiClient.get(`${base(companyType, companyId)}/${branchId}/`);

export const setBranchActive = async (companyType, companyId, branchId) =>
    apiClient.post(`${base(companyType, companyId)}/${branchId}/set_active/`);
