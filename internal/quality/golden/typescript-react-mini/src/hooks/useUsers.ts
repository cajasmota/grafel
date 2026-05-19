import { useState, useEffect, useCallback } from "react";
import { User } from "../types/user";

export function useUsers() {
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);
    const res = await fetch("/api/users");
    const data = (await res.json()) as User[];
    setUsers(data);
    setLoading(false);
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  return { users, loading, refresh };
}
