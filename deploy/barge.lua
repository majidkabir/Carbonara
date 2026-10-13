-- OSRM profile for inland waterway freight: the shortest path along
-- navigable rivers, canals and fairways in OpenStreetMap.
--
-- A waterway is navigable for freight when it has a CEMT class of I or
-- higher (the European inland waterway classification, key CEMT=*), is
-- tagged ship=yes/designated, is a canal used for transport
-- (usage=transportation), or is a fairway. Everything else, including
-- CEMT=0 (recreational) and ship=no, is left out, so routes never cut
-- through streams a barge can't use.
--
-- Weights are distance, so routes are shortest feasible distances (SFD, in
-- GLEC / ISO 14083 terms). Speeds are nominal and only used for durations,
-- which Carbonara ignores.

api_version = 4

local SPEED_KMH = 12

local navigable_waterway = { river = true, canal = true, fairway = true, tidal_channel = true }
local cemt_ok = {
  I = true, II = true, III = true, IV = true, Va = true, Vb = true,
  VIa = true, VIb = true, VIc = true, VII = true
}
local yes = { yes = true, designated = true }

function setup()
  return {
    properties = {
      weight_name                   = 'distance',
      max_speed_for_map_matching    = 40 / 3.6,
      u_turn_penalty                = 0,
      continue_straight_at_waypoint = false,
      use_turn_restrictions         = false,
      left_hand_driving             = false,
      traffic_light_penalty         = 0,
    },
    default_mode  = mode.ferry,
    default_speed = SPEED_KMH,
  }
end

local function navigable(way)
  local waterway = way:get_value_by_key('waterway')
  if not waterway or not navigable_waterway[waterway] then
    return false
  end
  local ship = way:get_value_by_key('ship')
  if ship == 'no' then
    return false
  end
  local cemt = way:get_value_by_key('CEMT')
  if cemt then
    return cemt_ok[cemt] == true
  end
  if ship and yes[ship] then
    return true
  end
  if waterway == 'fairway' then
    return true
  end
  if waterway == 'canal' and way:get_value_by_key('usage') == 'transportation' then
    return true
  end
  return false
end

function process_node(profile, node, result, relations)
end

function process_way(profile, way, result, relations)
  if not navigable(way) then
    return
  end
  result.forward_mode  = mode.ferry
  result.backward_mode = mode.ferry
  result.forward_speed  = SPEED_KMH
  result.backward_speed = SPEED_KMH
  -- metres per unit of weight: weight = distance
  result.forward_rate  = 1
  result.backward_rate = 1
  result.name = way:get_value_by_key('name') or ''
end

function process_turn(profile, turn)
  turn.duration = 0
  turn.weight = 0
end

return {
  setup = setup,
  process_way = process_way,
  process_node = process_node,
  process_turn = process_turn,
}
