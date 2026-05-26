# Crystal Kemal + Redis fixture.
# Demonstrates: HTTP endpoints, Redis SET/GET (DB access), Redis pub/sub producer, Redis pub/sub consumer.
require "kemal"
require "redis"
require "json"

# ── Domain ────────────────────────────────────────────────────────────────────

record Item, id : Int32, name : String, value : Float64 do
  include JSON::Serializable
end

# ── Redis client ─────────────────────────────────────────────────────────────

REDIS = Redis::Client.new(uri: URI.parse(ENV.fetch("REDIS_URL", "redis://localhost:6379")))

def redis_key(id : Int32) : String
  "item:#{id}"
end

def next_id : Int32
  REDIS.incr("item:id:seq").to_i32
end

def save_item(item : Item)
  REDIS.set(redis_key(item.id), item.to_json)
end

def load_item(id : Int32) : Item?
  raw = REDIS.get(redis_key(id))
  return nil unless raw
  Item.from_json(raw)
end

def delete_item(id : Int32) : Bool
  REDIS.del(redis_key(id)) > 0
end

def list_items : Array(Item)
  keys = REDIS.keys("item:*").reject { |k| k.includes?(":id:seq") }
  keys.compact_map { |k| (raw = REDIS.get(k)) ? Item.from_json(raw) : nil }
end

# ── Publisher ─────────────────────────────────────────────────────────────────

def publish_event(channel : String, payload : String)
  REDIS.publish(channel, payload)
end

# ── Subscriber ────────────────────────────────────────────────────────────────

def start_subscriber
  spawn do
    sub_redis = Redis::Client.new(uri: URI.parse(ENV.fetch("REDIS_URL", "redis://localhost:6379")))
    sub_redis.subscribe("items:created", "items:deleted") do |on|
      on.message do |channel, message|
        puts "Event on #{channel}: #{message}"
      end
    end
  end
end

# ── HTTP routes ───────────────────────────────────────────────────────────────

get "/health" do |env|
  env.response.content_type = "application/json"
  {status: "ok"}.to_json
end

get "/api/items" do |env|
  env.response.content_type = "application/json"
  list_items.to_json
end

get "/api/items/:id" do |env|
  id = env.params.url["id"].to_i32
  item = load_item(id)
  if item
    env.response.content_type = "application/json"
    item.to_json
  else
    halt env, status_code: 404, response: {error: "not found"}.to_json
  end
end

post "/api/items" do |env|
  env.response.content_type = "application/json"
  body = env.request.body.try(&.gets_to_end) || "{}"
  data = JSON.parse(body)
  id   = next_id
  item = Item.new(id: id, name: data["name"].as_s, value: data["value"].as_f)
  save_item(item)
  publish_event("items:created", item.to_json)
  env.response.status_code = 201
  item.to_json
end

delete "/api/items/:id" do |env|
  id = env.params.url["id"].to_i32
  if delete_item(id)
    publish_event("items:deleted", {id: id}.to_json)
    env.response.status_code = 204
    ""
  else
    halt env, status_code: 404, response: {error: "not found"}.to_json
  end
end

# ── Startup ───────────────────────────────────────────────────────────────────

start_subscriber
Kemal.run(port: 8080)
