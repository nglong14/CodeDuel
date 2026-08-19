-- Insert an entity into an ordered Redis structure exactly once, 
-- preserve its original position when its member representation changes, repair inconsistent state when necessary,
-- and generate strictly increasing ordering scores for new entries.

local old_member = redis.call('HGET', KEYS[2], ARGV[1])
if old_member then
  local old_score = redis.call('ZSCORE', KEYS[1], old_member)
  if old_score then
    if old_member ~= ARGV[2] then
      redis.call('ZREM', KEYS[1], old_member)
      redis.call('ZADD', KEYS[1], old_score, ARGV[2])
      redis.call('HSET', KEYS[2], ARGV[1], ARGV[2])
    end
    return {'0', string.format('%.0f', tonumber(old_score))}
  end
end

local now = redis.call('TIME')
local score = (tonumber(now[1]) * 1000000) + tonumber(now[2])
local last_score = tonumber(redis.call('GET', KEYS[3]) or '0')
if score <= last_score then
  score = last_score + 1
end

if old_member and old_member ~= ARGV[2] then
  redis.call('ZREM', KEYS[1], old_member)
end
redis.call('ZADD', KEYS[1], score, ARGV[2])
redis.call('HSET', KEYS[2], ARGV[1], ARGV[2])
redis.call('SET', KEYS[3], string.format('%.0f', score))
return {'1', string.format('%.0f', score)}
