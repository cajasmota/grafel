export interface Thing {
  readonly id: string;
  readonly code: string;
  readonly label: string;
}

export interface ThingPatch {
  readonly label?: string;
}
