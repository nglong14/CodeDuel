-- restores previosly matched users back into the queues only if their data is valid and poresence is still active
-- avoiding duplicate or conflicting queue entries
local restored = 0
for i = 1, #ARGV, 3 do
  local user_id = ARGV[i]
  local raw = ARGV[i + 1]
  local score = ARGV[i + 2]
  local ok, member = pcall(cjson.decode, raw)
  if ok
    and type(member) == 'table'
    and member.user_id == user_id
    and type(member.presence_key) == 'string'
    and redis.call('EXISTS', member.presence_key) == 1 then
    local mapped = redis.call('HGET', KEYS[2], user_id)
    if not mapped then
      redis.call('ZADD', KEYS[1], score, raw)
      redis.call('HSET', KEYS[2], user_id, raw)
      restored = restored + 1
    elseif mapped == raw and not redis.call('ZSCORE', KEYS[1], raw) then
      redis.call('ZADD', KEYS[1], score, raw)
      restored = restored + 1
    end
  end
end
return restored
