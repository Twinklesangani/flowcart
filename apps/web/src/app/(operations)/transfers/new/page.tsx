"use client";

import { useSearchParams } from "next/navigation";
import { TransferCreateView } from "../../../../components/workflow";

export default function NewTransferPage() {
  const params = useSearchParams();
  return <TransferCreateView source={params.get("source") ?? undefined} destination={params.get("destination") ?? undefined} product={params.get("product") ?? undefined} quantity={params.get("quantity") ?? undefined} />;
}