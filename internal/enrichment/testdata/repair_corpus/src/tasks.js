// repair_corpus/src/tasks.js
// KNOWN BUG: dynamic URL construction — the fetch target is a template
// literal whose base URL changes at runtime. The resolver emits a
// bug-extractor disposition because it cannot statically bind the template.
// The reference repair reclassifies this as dynamic.

async function fetchProposal(id) {
  const base = process.env.API_BASE;
  const url = `${base}/proposals/${id}`;  // BUG: template URL — extractor stub
  const response = await fetch(url);
  return response.json();
}

// KNOWN BUG: barrel re-export — ProposalCard is re-exported from the index
// barrel and the resolver loses track of the original module.
// The reference repair reclassifies as external ("@app/proposal-card").

import { ProposalCard } from "../components";  // BUG: barrel re-export stub
