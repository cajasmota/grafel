use tokio::sync::mpsc::Receiver;

pub async fn run_worker(mut rx: Receiver<String>) {
    while let Some(msg) = rx.recv().await {
        handle_message(msg).await;
    }
}

async fn handle_message(msg: String) {
    println!("got: {}", msg);
}
