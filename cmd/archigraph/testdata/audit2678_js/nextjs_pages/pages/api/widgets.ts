// Next.js pages-router API route. The route is implicit:
//   pages/api/widgets.ts  →  /api/widgets
// The handler IS in this file (export default), so source_file must be
// this very file and source_line must point to the function def.

import type { NextApiRequest, NextApiResponse } from "next";

export default function widgetsHandler(
  req: NextApiRequest,
  res: NextApiResponse,
): void {
  if (req.method === "GET") {
    res.status(200).json([{ id: 1 }]);
    return;
  }
  res.status(405).end();
}
