import { User } from '../../../users/models/user.entity';

export interface InspectorResponse {
  id: number;
  username: string;
  email: string | null;
  isActive: boolean;
  groups: null;
  inspectionGroups: null;
}

export interface PaginatedInspectorResponse {
  count: number;
  next: string | null;
  previous: string | null;
  results: InspectorResponse[];
}

export function toInspectorResponse(entity: User): InspectorResponse {
  return {
    id: entity.id,
    username: entity.username,
    email: entity.email,
    isActive: entity.status === 'active',
    groups: null,
    inspectionGroups: null,
  };
}
