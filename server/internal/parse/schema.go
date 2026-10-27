package parse

import "encoding/json"

// Schema is the JSON Schema of the model's answer (PLAN.md section 13), in the
// strict form providers accept: every property present, optional ones nullable.
var Schema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["kind", "title", "date", "time_from", "time_to", "duration_min", "deadline", "recurrence", "category_guess", "missing"],
  "properties": {
    "kind": {"type": "string", "enum": ["event", "task"]},
    "title": {"type": "string"},
    "date": {"anyOf": [{"type": "null"}, {"$ref": "#/$defs/date"}]},
    "time_from": {"type": ["string", "null"]},
    "time_to": {"type": ["string", "null"]},
    "duration_min": {"type": ["integer", "null"]},
    "deadline": {"anyOf": [{"type": "null"}, {"$ref": "#/$defs/date"}]},
    "recurrence": {"type": ["string", "null"]},
    "category_guess": {"type": "string", "enum": ["study", "work", "hobby", "rest", "chores", "other"]},
    "missing": {"type": "array", "items": {"type": "string", "enum": ["date", "time_from", "time_to", "duration_min", "deadline"]}}
  },
  "$defs": {
    "date": {
      "type": "object",
      "additionalProperties": false,
      "required": ["type", "days", "value", "week"],
      "properties": {
        "type": {"type": "string", "enum": ["relative", "weekday", "absolute"]},
        "days": {"type": ["integer", "null"]},
        "value": {"type": ["string", "null"]},
        "week": {"type": ["string", "null"], "enum": ["this", "next", null]}
      }
    }
  }
}`)

const SchemaName = "parse_phrase"
