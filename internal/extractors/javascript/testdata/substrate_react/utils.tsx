// Utility module imported by UserDashboard.tsx.
// Proves import_resolution_quality: cross-file named export consumed by
// a peer component in the same directory.
export function formatUserName(first: string, last: string): string {
  return `${first} ${last}`.trim();
}
