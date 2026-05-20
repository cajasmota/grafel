from workers.billing import charge_card, send_receipt


def process_checkout(user_id: int, amount: float) -> None:
    """Producer: enqueue billing tasks from the checkout flow."""
    charge_card.send(user_id, amount)
    send_receipt.send_with_options(args=[user_id, amount], delay=2000)
