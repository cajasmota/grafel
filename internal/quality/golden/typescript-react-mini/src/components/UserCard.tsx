import { User } from "../types/user";

interface UserCardProps {
  user: User;
  onSelect?: (id: string) => void;
}

export function UserCard({ user, onSelect }: UserCardProps) {
  const handleClick = () => onSelect?.(user.id);
  return (
    <div className="user-card" onClick={handleClick}>
      <strong>{user.name}</strong>
      <span>{user.email}</span>
    </div>
  );
}
