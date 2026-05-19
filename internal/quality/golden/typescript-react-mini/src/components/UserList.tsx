import { useUsers } from "../hooks/useUsers";
import { UserCard } from "./UserCard";

interface UserListProps {
  onPick?: (id: string) => void;
}

export function UserList({ onPick }: UserListProps) {
  const { users, loading } = useUsers();
  if (loading) return <p>loading…</p>;
  return (
    <ul>
      {users.map((u) => (
        <UserCard key={u.id} user={u} onSelect={onPick} />
      ))}
    </ul>
  );
}
