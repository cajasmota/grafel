def sync(self, request):
    audit.log("start")
    if request.data.get("force"):
        notifier.send(request.user)
    for row in rows:
        mailer.deliver(row)
    return Response({"ok": True})
