-- Scans the earliest queue entries, removes malformed/lstale/inconsistent members
-- selects the first two valid active users, atomically removes them from the queue,
-- returns them as a match
local function valid_uuid(value)
  return type(value) == 'string'
    and value ~= '00000000-0000-0000-0000-000000000000'
    and string.match(value, '^[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]%-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]%-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]%-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]%-[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]$')
end

local function remove_member(raw, user_id)
  redis.call('ZREM', KEYS[1], raw)
  if user_id and redis.call('HGET', KEYS[2], user_id) == raw then
    redis.call('HDEL', KEYS[2], user_id)
  end
end

local entries = redis.call('ZRANGE', KEYS[1], 0, tonumber(ARGV[1]) - 1, 'WITHSCORES')
local selected = {}

for i = 1, #entries, 2 do
  local raw = entries[i]
  local score = entries[i + 1]
  local ok, member = pcall(cjson.decode, raw)
  local user_id = ok and type(member) == 'table' and member.user_id or nil
  local valid = false
  if ok and type(member) == 'table' and valid_uuid(user_id) then
    local presence_prefix = 'codeduel:presence:' .. user_id .. ':'
    valid = type(member.presence_key) == 'string'
      and string.sub(member.presence_key, 1, string.len(presence_prefix)) == presence_prefix
      and valid_uuid(string.sub(member.presence_key, string.len(presence_prefix) + 1))
      and type(member.route) == 'string'
      and member.route == 'codeduel:user:' .. user_id
      and type(member.rating) == 'number'
  end

  if not valid then
    remove_member(raw, user_id)
  elseif redis.call('HGET', KEYS[2], user_id) ~= raw then
    redis.call('ZREM', KEYS[1], raw)
  elseif redis.call('EXISTS', member.presence_key) == 0 then
    remove_member(raw, user_id)
  elseif #selected < 4 then
    table.insert(selected, raw)
    table.insert(selected, score)
  end
end

if #selected < 4 then
  return {}
end

for i = 1, #selected, 2 do
  local raw = selected[i]
  local member = cjson.decode(raw)
  remove_member(raw, member.user_id)
end
return selected
