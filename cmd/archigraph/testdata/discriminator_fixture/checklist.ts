// Fixture for issue #2670 — verify the production extract pipeline emits
// DISCRIMINATES_ON edges for discriminator-pattern comparisons in real files.

export function processChecklist(checklistType: number, role: string, status: string) {
  const isCat5 = checklistType === 2;
  const isAdmin = role === 'admin';
  const isActive = status === 'active';
  return isCat5 && isAdmin && isActive;
}
