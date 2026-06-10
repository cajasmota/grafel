@shared_task(
    bind=True,
    queue="ecb_ocr",
    max_retries=3,
    default_retry_delay=60,
    acks_late=True,
    time_limit=_TASK_TIME_LIMIT,
    soft_time_limit=_TASK_SOFT_TIME_LIMIT,
)
def process_ecb_pdf_job(self, payload: dict):
    if not settings.ECB_PDF_PIPELINE_ENABLED:
        logger.warning(
            "process_ecb_pdf_job: pipeline disabled — dropping task. violation_number=%s",
            payload.get("violation_number"),
        )
        return

    t_total = time.monotonic()

    violation_number = payload.get("violation_number")
    s3_bucket = payload.get("s3_bucket")
    s3_key = payload.get("s3_key")

    missing = [f for f, v in [("violation_number", violation_number), ("s3_bucket", s3_bucket), ("s3_key", s3_key)] if not v]
    if missing:
        logger.error(
            "process_ecb_pdf_job: missing required fields=%s — dropping task. payload=%s",
            missing,
            payload,
        )
        return

    sqs_env = payload.get("env")
    should_write_to_db = settings.ECB_OCR_WRITE_TO_CONTROLLER_DB and sqs_env == "prod"

    logger.info(
        "process_ecb_pdf_job: starting. violation_number=%s s3_bucket=%s s3_key=%s task_id=%s",
