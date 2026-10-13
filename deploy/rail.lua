-- OSRM profile for rail freight: the shortest path along railway lines in
-- OpenStreetMap that freight trains can use.
--
-- Included: railway=rail (main, branch and industrial lines, plus the
-- sidings, spurs and yards that reach freight terminals). Left out: trams,
-- light rail, subways, narrow gauge, funiculars, tourist lines, and lines
-- that are disused, abandoned, under construction or only proposed.
--
-- Trains can't turn sharply or reverse at a junction, so turns of more
-- than 60 degrees and u-turns get a penalty of 100 km: a route only uses
-- them when there is no other way.
--
-- Weights are distance, so routes are shortest feasible distances (SFD, in
-- GLEC / ISO 14083 terms). Speeds are nominal and only used for durations,
-- which Carbonara ignores.

api_version = 4

local SPEED_KMH = 60
local SHARP_TURN_DEG = 60
local TURN_PENALTY_M = 100000

local excluded_usage = { tourism = true }

function setup()
  return {
    properties = {
      weight_name                   = 'distance',
      max_speed_for_map_matching    = 160 / 3.6,
      u_turn_penalty                = TURN_PENALTY_M,
      continue_straight_at_waypoint = true,
      use_turn_restrictions         = false,
      left_hand_driving             = false,
      traffic_light_penalty         = 0,
    },
    default_mode  = mode.train,
    default_speed = SPEED_KMH,
  }
end

local function usable(way)
  if way:get_value_by_key('railway') ~= 'rail' then
    return false
  end
  local usage = way:get_value_by_key('usage')
  if usage and excluded_usage[usage] then
    return false
  end
  local gauge = way:get_value_by_key('gauge')
  -- Narrow gauge lines are usually tagged railway=narrow_gauge; this also
  -- drops railway=rail lines tagged with a gauge under a metre.
  if gauge then
    local mm = tonumber(gauge)
    if mm and mm < 1000 then
      return false
    end
  end
  return true
end

function process_node(profile, node, result, relations)
end

function process_way(profile, way, result, relations)
  if not usable(way) then
    return
  end
  result.forward_mode  = mode.train
  result.backward_mode = mode.train
  result.forward_speed  = SPEED_KMH
  result.backward_speed = SPEED_KMH
  -- metres per unit of weight: weight = distance
  result.forward_rate  = 1
  result.backward_rate = 1
  result.name = way:get_value_by_key('name') or ''
end

function process_turn(profile, turn)
  if turn.is_u_turn or math.abs(turn.angle) > SHARP_TURN_DEG then
    turn.weight = TURN_PENALTY_M
  else
    turn.weight = 0
  end
  turn.duration = 0
end

return {
  setup = setup,
  process_way = process_way,
  process_node = process_node,
  process_turn = process_turn,
}
