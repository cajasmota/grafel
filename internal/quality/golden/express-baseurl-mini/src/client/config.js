// The base-URL constant lives in its own module. Cross-FILE is the whole
// point: the JS extractor already folds a base URL declared in the SAME
// file as the fetch call, so a same-file fixture would grade nothing.
export const BASE = '/api';

// Fully-qualified form. The fold must strip scheme + host before splicing,
// otherwise the folded path is `http://svc.internal/api/health` and matches
// no producer-side route.
export const API_ORIGIN = 'http://svc.internal/api';

// Negative control: a second exported constant that is NOT a URL prefix and
// must never be substituted into a path.
export const TIMEOUT_MS = '5000';
