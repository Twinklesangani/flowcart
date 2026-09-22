import { ProtectedShell } from "../../components/app-shell";

export default function OperationsLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <ProtectedShell>{children}</ProtectedShell>;
}