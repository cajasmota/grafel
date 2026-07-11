<?php
require __DIR__ . '/../../vendor/autoload.php';

use Psr\Http\Message\ResponseInterface as Response;
use Psr\Http\Message\ServerRequestInterface as Request;
use Slim\Factory\AppFactory;

$app = AppFactory::create();

$app->get('/invoices', function (Request $request, Response $response) {
    $response->getBody()->write(json_encode([]));
    return $response;
});

$app->post('/invoices', function (Request $request, Response $response) {
    $data = $request->getParsedBody();
    $response->getBody()->write(json_encode($data));
    return $response;
});

$app->get('/invoices/:id', function (Request $request, Response $response, $args) {
    $response->getBody()->write(json_encode(['id' => $args['id']]));
    return $response;
});

$app->put('/invoices/:id', function (Request $request, Response $response, $args) {
    $response->getBody()->write(json_encode(['id' => $args['id']]));
    return $response;
});

$app->delete('/invoices/:id', function (Request $request, Response $response, $args) {
    return $response->withStatus(204);
});

$app->run();
