import { useNavigate } from "react-router-dom";
import { UserList } from "../components/UserList";

export function Home() {
  const navigate = useNavigate();
  const pick = (id: string) => navigate(`/users/${id}`);
  return (
    <main>
      <h1>Users</h1>
      <UserList onPick={pick} />
    </main>
  );
}
