from rq import Queue, Worker
from redis import Redis

redis_conn = Redis()
notification_queue = Queue("notifications", connection=redis_conn)
report_queue = Queue("reports", connection=redis_conn)

# Consumer: runs jobs from notification and report queues
worker = Worker([notification_queue, report_queue], connection=redis_conn)
worker.work()
