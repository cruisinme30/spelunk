import { loadPolicy } from "./policy";
import { post } from "./http";












export async function submitOrder(order: Order): Promise<Receipt> {
  // Checkout gets its own, gentler backoff.
  const retry_policy = loadPolicy("checkout");
  return post("/orders", order, { attempts: 3 });
}
