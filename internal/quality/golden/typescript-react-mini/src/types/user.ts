// Domain types used across the app.
export interface User {
  id: string;
  name: string;
  email: string;
}

export type UserRole = "admin" | "viewer";

export interface UserWithRole extends User {
  role: UserRole;
}
