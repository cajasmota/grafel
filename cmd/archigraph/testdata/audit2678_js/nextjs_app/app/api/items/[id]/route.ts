// Next.js app-router API route. The route is implicit:
//   app/api/items/[id]/route.ts  →  /api/items/{id}
// Each verb is a named export. After #2678 each verb's source_line must
// land on the corresponding function declaration.

import { NextRequest, NextResponse } from "next/server";

export async function GET(
  req: NextRequest,
  ctx: { params: { id: string } },
): Promise<NextResponse> {
  return NextResponse.json({ id: ctx.params.id });
}

export async function DELETE(
  req: NextRequest,
  ctx: { params: { id: string } },
): Promise<NextResponse> {
  return NextResponse.json({ id: ctx.params.id, deleted: true });
}
