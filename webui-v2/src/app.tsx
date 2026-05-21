/* ============================================================
   App — providers + theme sync + router.
   Provider order: QueryClient → Tooltip → ThemeSync → Router.
   ============================================================ */

import { RouterProvider } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Toaster } from "sonner";
import { TooltipProvider } from "@/components/ui";
import { useThemeSync } from "@/hooks/use-theme-sync";
import { router } from "@/routes/router";

const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 30_000, refetchOnWindowFocus: false } },
});

export function App() {
  useThemeSync();
  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={200}>
        <RouterProvider router={router} />
        <Toaster position="bottom-center" />
      </TooltipProvider>
    </QueryClientProvider>
  );
}
