import { TransferDetailView } from "../../../../components/workflow";

export default async function TransferPage({ params }: { params: Promise<{ transferID: string }> }) {
  const { transferID } = await params;
  return <TransferDetailView transferID={transferID} />;
}