import { Navigate } from "react-router-dom"

export function IndexRoute() {
  return <Navigate to="/graph/fixture-a" replace />
}
