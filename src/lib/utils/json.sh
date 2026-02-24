#!/bin/bash
# dot-agents/lib/utils/json.sh
# JSON manipulation utilities (jq wrappers with fallbacks)

# Check if jq is available
_json_has_jq() {
  command -v jq &>/dev/null
}

# Read a JSON file
# Usage: json_read "/path/to/file.json"
json_read() {
  local file="$1"
  if [ -f "$file" ]; then
    cat "$file"
  else
    echo "{}"
  fi
}

# Get a value from JSON
# Usage: json_get '{"key": "value"}' ".key"
# Usage: json_get_file "/path/to/file.json" ".key"
json_get() {
  local json="$1"
  local path="$2"

  if _json_has_jq; then
    echo "$json" | jq -r "$path // empty" 2>/dev/null
  else
    # Basic fallback for simple cases like .version or .projects
    local key="${path#.}"
    echo "$json" | grep -o "\"$key\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | sed 's/.*: *"\([^"]*\)".*/\1/' | head -1
  fi
}

json_get_file() {
  local file="$1"
  local path="$2"
  json_get "$(json_read "$file")" "$path"
}

# Check if a key exists in JSON
# Usage: json_has '{"key": "value"}' ".key"
json_has() {
  local json="$1"
  local path="$2"

  if _json_has_jq; then
    echo "$json" | jq -e "$path" &>/dev/null
  else
    local key="${path#.}"
    echo "$json" | grep -q "\"$key\""
  fi
}

# Get array length
# Usage: json_array_length '["a","b"]' "."
json_array_length() {
  local json="$1"
  local path="${2:-.}"

  if _json_has_jq; then
    echo "$json" | jq -r "$path | length" 2>/dev/null || echo "0"
  else
    # Basic fallback - count commas + 1 for non-empty arrays
    echo "0"
  fi
}

# Get object keys
# Usage: json_keys '{"a":1,"b":2}' "."
json_keys() {
  local json="$1"
  local path="${2:-.}"

  if _json_has_jq; then
    echo "$json" | jq -r "$path | keys[]" 2>/dev/null
  else
    echo ""
  fi
}

# Set a value in JSON (returns new JSON)
# Usage: json_set '{"key": "old"}' ".key" '"new"'
json_set() {
  local json="$1"
  local path="$2"
  local value="$3"

  if _json_has_jq; then
    echo "$json" | jq "$path = $value" 2>/dev/null
  else
    log_error "jq is required for JSON modification"
    echo "$json"
  fi
}

# Add an object to a path
# Usage: json_set_object '{}' ".projects.myproject" '{"path": "/foo"}'
json_set_object() {
  local json="$1"
  local path="$2"
  local object="$3"

  if _json_has_jq; then
    echo "$json" | jq "$path = $object" 2>/dev/null
  else
    log_error "jq is required for JSON modification"
    echo "$json"
  fi
}

# Delete a key from JSON
# Usage: json_delete '{"a":1,"b":2}' ".b"
json_delete() {
  local json="$1"
  local path="$2"

  if _json_has_jq; then
    echo "$json" | jq "del($path)" 2>/dev/null
  else
    log_error "jq is required for JSON modification"
    echo "$json"
  fi
}

# Pretty print JSON
# Usage: json_pretty '{"key":"value"}'
json_pretty() {
  local json="$1"

  if _json_has_jq; then
    echo "$json" | jq '.' 2>/dev/null
  else
    echo "$json"
  fi
}

# Validate JSON
# Usage: json_valid '{"key":"value"}' && echo "valid"
json_valid() {
  local json="$1"

  if _json_has_jq; then
    echo "$json" | jq -e '.' &>/dev/null
  else
    # Basic check - has matching braces
    local open_braces close_braces
    open_braces=$(echo "$json" | tr -cd '{' | wc -c)
    close_braces=$(echo "$json" | tr -cd '}' | wc -c)
    [ "$open_braces" -eq "$close_braces" ] && [ "$open_braces" -gt 0 ]
  fi
}

# Write JSON to file with pretty formatting
# Usage: json_write_file "/path/to/file.json" '{"key":"value"}'
json_write_file() {
  local file="$1"
  local json="$2"

  if _json_has_jq; then
    echo "$json" | jq '.' > "$file"
  else
    echo "$json" > "$file"
  fi
}

# Read config.json and get projects object
# Usage: config_get_projects
config_get_projects() {
  local config_file="$AGENTS_HOME/config.json"
  json_get_file "$config_file" ".projects"
}

# Add a project to config.json
# Usage: config_add_project "project-name" "/path/to/project"
config_add_project() {
  local name="$1"
  local path="$2"
  local config_file="$AGENTS_HOME/config.json"

  if ! _json_has_jq; then
    log_error "jq is required to modify config.json"
    return 1
  fi

  local json
  json=$(json_read "$config_file")

  # Create project object
  local project_json
  project_json=$(jq -n --arg path "$path" '{
    path: $path,
    added: now | strftime("%Y-%m-%dT%H:%M:%SZ")
  }')

  # Add to projects
  json=$(echo "$json" | jq --arg name "$name" --argjson proj "$project_json" '.projects[$name] = $proj')

  if [ "$DRY_RUN" = true ]; then
    log_dry "add project '$name' to config.json"
  else
    json_write_file "$config_file" "$json"
  fi
}

# Remove a project from config.json
# Usage: config_remove_project "project-name"
config_remove_project() {
  local name="$1"
  local config_file="$AGENTS_HOME/config.json"

  if ! _json_has_jq; then
    log_error "jq is required to modify config.json"
    return 1
  fi

  local json
  json=$(json_read "$config_file")
  json=$(echo "$json" | jq --arg name "$name" 'del(.projects[$name])')

  if [ "$DRY_RUN" = true ]; then
    log_dry "remove project '$name' from config.json"
  else
    json_write_file "$config_file" "$json"
  fi
}

# List all project names
# Usage: config_list_projects
config_list_projects() {
  local config_file="$AGENTS_HOME/config.json"
  json_get_file "$config_file" ".projects | keys[]"
}

# Get a project's path
# Usage: config_get_project_path "project-name"
config_get_project_path() {
  local name="$1"
  local config_file="$AGENTS_HOME/config.json"
  json_get_file "$config_file" ".projects.\"$name\".path"
}

export -f json_read json_get json_get_file json_has json_array_length json_keys
export -f json_set json_set_object json_delete json_pretty json_valid json_write_file
export -f config_get_projects config_add_project config_remove_project config_list_projects config_get_project_path
