import dramatiq

@dramatiq.actor(queue_name="billing", max_retries=3)
def charge_card(user_id: int, amount: float) -> None:
    """Consumer: charge a card in the background."""
    pass


@dramatiq.actor
def send_receipt(user_id: int, amount: float) -> None:
    """Consumer: send a payment receipt."""
    pass
