<?php

use GuzzleHttp\Client;
use Illuminate\Support\Facades\Http;

class BillingService
{
    public function createOrder(array $data): array
    {
        $client = new Client();
        $response = $client->post('http://orders-service/api/orders', ['json' => $data]);
        return json_decode($response->getBody(), true);
    }

    public function sendNotification(string $userId, string $event): void
    {
        Http::post('http://notifications-service/api/notifications', [
            'user_id' => $userId,
            'event'   => $event,
        ]);
    }

    public function getOrder(string $id): array
    {
        $client = new Client();
        $response = $client->get('http://orders-service/api/orders/' . $id);
        return json_decode($response->getBody(), true);
    }
}
