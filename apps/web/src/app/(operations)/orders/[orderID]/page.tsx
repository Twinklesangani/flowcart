import { OrderDetailView } from "../../../../components/operations";

export default async function OrderPage({ params }: { params: Promise<{ orderID: string }> }) {
  const { orderID } = await params;
  return <OrderDetailView orderID={orderID} />;
}