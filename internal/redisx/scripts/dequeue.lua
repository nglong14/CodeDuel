-- Remove only the exact queued connection. A reconnect must not be cancelled
-- by an older connection for the same user.
local current = redis.call('HGET', KEYS[2], ARGV[1])
if current ~= ARGV[2] then
  return 0
end

local removed = redis.call('ZREM', KEYS[1], current)
redis.call('HDEL', KEYS[2], ARGV[1])
return removed
