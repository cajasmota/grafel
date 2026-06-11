package svc

func Sync(force bool, rows []Row) error {
	audit.Log("start")
	if force {
		notifier.Send(rows)
	}
	for _, row := range rows {
		mailer.Deliver(row)
	}
	return nil
}
