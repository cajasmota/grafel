<?php

namespace App\Http\Controllers;

use Symfony\Component\HttpKernel\Exception\HttpException;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Response;

// UserController::store is a representative Laravel/Symfony controller action
// with an env-gate (env() config helper), an early-return 400 guard, a 409
// conflict guard inside the try, and a try/catch that re-throws an
// HttpException(500). Used by effects_branches_4448_test.go to exercise the
// REAL effects MCP handler over a PHP function on disk.
class UserController
{
    public function store(Request $request)
    {
        if (!env('SIGNUP_ENABLED')) {
            return response()->json(['error' => 'signup disabled'], 503);
        }

        if ($request->input('email') === null) {
            return response()->json(['error' => 'email required'], 400);
        }

        try {
            if (User::where('email', $request->email)->exists()) {
                return response()->json(['error' => 'email taken'], 409);
            }

            $user = User::create($request->all());

            return response()->json($user, 201);
        } catch (\Exception $e) {
            Log::error($e->getMessage());
            throw new HttpException(500, 'could not create user');
        }
    }
}
