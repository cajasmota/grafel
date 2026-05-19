import { useNavigate, useParams } from "react-router-dom";
import { useAuth } from "../context/AuthContext";

export function UserDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { user } = useAuth();
  const goBack = () => navigate(-1);
  return (
    <section>
      <h1>User {id}</h1>
      {user && <p>Viewing as {user.name}</p>}
      <button onClick={goBack}>back</button>
    </section>
  );
}
