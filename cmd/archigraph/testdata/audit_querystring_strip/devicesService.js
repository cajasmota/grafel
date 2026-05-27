// Test file for query-string stripping in HTTP path extraction (issue #2710)
// Verifies that canonical paths are extracted without query-string segments
// while query-string templates are stashed in Properties for telemetry/ranking.

import axios from 'axios';

const apiClient = axios.create({
  baseURL: 'http://api.example.com'
});

export async function getDevices() {
  // Template-literal with query-string segment and interpolation.
  // Expected canonical path: /devices (no query string)
  // Query template stashed: queryParams.toString()
  const response = await apiClient.get(`/devices?${queryParams.toString()}`);
  return response.data;
}

export async function searchUsers(searchTerm) {
  // Template literal with fixed query string + parameter.
  // Expected canonical path: /users (no query string)
  // Query template stashed: q=searchTerm&limit=10
  const response = await axios.get(
    `/users?q=${searchTerm}&limit=10`
  );
  return response.data;
}

export async function getFilteredBuildings(filters) {
  // Mixed static query string prefix and interpolated value.
  // Expected canonical path: /buildings (no query string)
  // Query template stashed: filter=value&${filters.toString()}
  const response = await apiClient.post(
    `/buildings?filter=value&${filters.toString()}`,
    { metadata: 'test' }
  );
  return response.data;
}

export async function fetchWithFetch() {
  // Fetch with template literal containing URLSearchParams interpolation.
  // Expected canonical path: /api/v1/contracts (no query string)
  // Query template stashed: version=2&status=active&${queryParams}
  const qp = new URLSearchParams({ version: '2', status: 'active' });
  const response = await fetch(
    `/api/v1/contracts?version=2&status=active&${qp.toString()}`
  );
  return response.json();
}

export async function getDocuments(id, includeMetadata) {
  // Path with path parameter and query string.
  // Expected canonical path: /documents/{id} (no query string)
  // Query template stashed: includeMetadata={includeMetadata}
  const response = await axios.get(
    `/documents/${id}?includeMetadata=${includeMetadata}`
  );
  return response.data;
}

export async function listChecklists(pagination) {
  // Query string using .toString() method call on an expression.
  // Expected canonical path: /checklists (no query string)
  // The .toString() is part of the query template, not the path.
  const response = await apiClient.get(
    `/checklists?${pagination.toString()}`
  );
  return response.data;
}

// Control case: static path without query string (should pass through unchanged)
export async function getStaticPath() {
  const response = await axios.get('/api/v1/users');
  return response.data;
}

// Control case: path with no query string in template literal
export async function getDynamicPath(id) {
  const response = await axios.get(`/users/${id}/profile`);
  return response.data;
}
