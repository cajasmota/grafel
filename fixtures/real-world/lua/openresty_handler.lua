-- Source: https://github.com/openresty/lua-nginx-module (synthetic based on real OpenResty patterns) | License: BSD-2-Clause

local cjson = require "cjson"
local redis = require "resty.redis"
local http = require "resty.http"

local _M = {}

-- Constants
local CACHE_TTL = 300  -- 5 minutes
local MAX_RETRIES = 3
local RATE_LIMIT_WINDOW = 60  -- seconds
local RATE_LIMIT_MAX = 100    -- requests per window

-- Connect to Redis
local function get_redis()
    local red = redis:new()
    red:set_timeouts(1000, 1000, 1000)

    local ok, err = red:connect(
        os.getenv("REDIS_HOST") or "127.0.0.1",
        tonumber(os.getenv("REDIS_PORT")) or 6379
    )
    if not ok then
        ngx.log(ngx.ERR, "failed to connect to redis: ", err)
        return nil, err
    end

    local password = os.getenv("REDIS_PASSWORD")
    if password then
        local res, err = red:auth(password)
        if not res then
            return nil, err
        end
    end

    return red
end

-- Release Redis connection back to pool
local function release_redis(red)
    local ok, err = red:set_keepalive(10000, 100)
    if not ok then
        ngx.log(ngx.WARN, "failed to set keepalive: ", err)
        red:close()
    end
end

-- Rate limiting via sliding window
function _M.check_rate_limit(key)
    local red, err = get_redis()
    if not red then
        -- Fail open if Redis unavailable
        return true
    end

    local now = ngx.now()
    local window_start = now - RATE_LIMIT_WINDOW

    red:multi()
    red:zremrangebyscore(key, "-inf", window_start)
    red:zadd(key, now, now .. math.random(1, 1000000))
    red:zcard(key)
    red:expire(key, RATE_LIMIT_WINDOW * 2)
    local res, err = red:exec()

    release_redis(red)

    if not res then
        return true
    end

    local count = res[3]
    return count <= RATE_LIMIT_MAX, count
end

-- Cache-aside pattern
function _M.get_cached(cache_key, fetch_fn)
    local red, err = get_redis()
    if red then
        local cached, err = red:get(cache_key)
        if cached and cached ~= ngx.null then
            release_redis(red)
            return cjson.decode(cached)
        end
        release_redis(red)
    end

    -- Cache miss — fetch from source
    local data, err = fetch_fn()
    if not data then
        return nil, err
    end

    -- Store in cache
    if red then
        local red2, _ = get_redis()
        if red2 then
            red2:setex(cache_key, CACHE_TTL, cjson.encode(data))
            release_redis(red2)
        end
    end

    return data
end

-- Main request handler
function _M.handle_request()
    -- Extract org from JWT
    local auth_header = ngx.req.get_headers()["Authorization"]
    if not auth_header then
        ngx.status = 401
        ngx.say(cjson.encode({error = "Missing Authorization header"}))
        return ngx.exit(401)
    end

    local token = auth_header:match("Bearer (.+)")
    if not token then
        ngx.status = 401
        ngx.say(cjson.encode({error = "Invalid Authorization format"}))
        return ngx.exit(401)
    end

    -- Check rate limit
    local org_id = ngx.var.arg_org_id or "anonymous"
    local rate_key = "rate:" .. org_id .. ":" .. ngx.var.uri
    local allowed, count = _M.check_rate_limit(rate_key)

    ngx.header["X-RateLimit-Remaining"] = math.max(0, RATE_LIMIT_MAX - (count or 0))
    ngx.header["X-RateLimit-Limit"] = RATE_LIMIT_MAX

    if not allowed then
        ngx.status = 429
        ngx.say(cjson.encode({error = "Rate limit exceeded"}))
        return ngx.exit(429)
    end

    -- Proxy to upstream
    local httpc = http.new()
    httpc:set_timeouts(2000, 60000, 60000)

    local upstream = os.getenv("UPSTREAM_URL") or "http://127.0.0.1:8080"
    local res, err = httpc:request_uri(upstream .. ngx.var.request_uri, {
        method = ngx.req.get_method(),
        headers = ngx.req.get_headers(),
        body = ngx.req.get_body_data(),
        ssl_verify = false,
    })

    if not res then
        ngx.log(ngx.ERR, "upstream request failed: ", err)
        ngx.status = 502
        ngx.say(cjson.encode({error = "Bad Gateway"}))
        return ngx.exit(502)
    end

    ngx.status = res.status
    for k, v in pairs(res.headers) do
        ngx.header[k] = v
    end
    ngx.say(res.body)
end

return _M
