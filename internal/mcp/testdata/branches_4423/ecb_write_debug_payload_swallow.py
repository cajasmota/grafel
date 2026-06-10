def _write_debug_payload(violation_number: str, task_id: str, sqs_payload: dict, ocr_result: dict) -> None:
    """
    Log and save the full OCR result when ECB_OCR_DEBUG_PAYLOADS is enabled.
    Never raises — a file write failure must not affect task success/failure.
    """
    try:
        loggable = {k: v for k, v in ocr_result.items() if k not in ("raw_text", "pages")}
        logger.info(
            "process_ecb_pdf_job: OCR extracted payload — violation_number=%s task_id=%s\n%s",
            violation_number,
            task_id,
            json.dumps(loggable, default=str, indent=2),
        )

        _DEBUG_PAYLOAD_DIR.mkdir(parents=True, exist_ok=True)

        safe_violation = re.sub(r"[^\w.-]", "_", str(violation_number))
        output_path = _DEBUG_PAYLOAD_DIR / f"{safe_violation}_{task_id}.json"

        file_payload = {
            "violation_number": violation_number,
            "task_id": str(task_id),
            "sqs_payload": sqs_payload,
            "ocr_result": ocr_result,
        }
        output_path.write_text(json.dumps(file_payload, default=str, indent=2), encoding="utf-8")
        logger.info("process_ecb_pdf_job: debug payload saved. path=%s", output_path)

    except Exception as exc:
        logger.warning(
            "process_ecb_pdf_job: debug payload write failed (non-fatal). violation_number=%s error=%s",
            violation_number,
            exc,
        )

