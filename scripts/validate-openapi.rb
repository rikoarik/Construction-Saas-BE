#!/usr/bin/env ruby
# Validate the repository's OpenAPI document without downloading tooling.
require "yaml"

path = ARGV.fetch(0, "api/openapi.yaml")
doc = YAML.safe_load(File.read(path), permitted_classes: [], aliases: false)
errors = []

unless doc.is_a?(Hash) && doc["openapi"].to_s.start_with?("3.")
  abort("#{path}: missing supported OpenAPI version")
end
unless doc["info"].is_a?(Hash) && doc["info"]["title"].is_a?(String) && doc["info"]["version"].is_a?(String)
  errors << "info.title and info.version are required strings"
end
paths = doc["paths"]
errors << "paths must be a non-empty mapping" unless paths.is_a?(Hash) && !paths.empty?

resolve = lambda do |ref|
  unless ref.start_with?("#/")
    errors << "external reference not allowed in CI: #{ref}"
    next
  end
  ref.delete_prefix("#/").split("/").map { |part| part.gsub("~1", "/").gsub("~0", "~") }.reduce(doc) do |node, part|
    node.is_a?(Hash) ? node[part] : nil
  end
end
parameter_names = lambda do |parameters|
  Array(parameters).map do |parameter|
    parameter = resolve.call(parameter["$ref"]) if parameter.is_a?(Hash) && parameter["$ref"].is_a?(String)
    parameter["name"] if parameter.is_a?(Hash) && parameter["in"] == "path"
  end.compact
end

operations = []
(paths || {}).each do |template, item|
  unless template.start_with?("/") && item.is_a?(Hash)
    errors << "invalid path item #{template.inspect}"
    next
  end
  path_parameters = template.scan(/\{([^}]+)\}/).flatten
  item.each do |method, operation|
    next if method == "parameters" || method == "$ref"
    unless %w[get put post delete options head patch trace].include?(method) && operation.is_a?(Hash)
      errors << "invalid operation #{method.inspect} at #{template}"
      next
    end
    operation_id = operation["operationId"]
    errors << "#{method.upcase} #{template} is missing operationId" unless operation_id.is_a?(String) && !operation_id.empty?
    operations << operation_id if operation_id
    declared = parameter_names.call(item["parameters"]) + parameter_names.call(operation["parameters"])
    (path_parameters - declared).each { |name| errors << "#{method.upcase} #{template} has undeclared path parameter #{name}" }
    responses = operation["responses"]
    errors << "#{method.upcase} #{template} is missing responses" unless responses.is_a?(Hash) && !responses.empty?
  end
end
operations.group_by(&:itself).each do |id, entries|
  errors << "duplicate operationId #{id}" if entries.size > 1
end

collect_references = lambda do |node|
  case node
  when Hash
    refs = []
    refs << node["$ref"] if node["$ref"].is_a?(String)
    node.each_value { |value| refs.concat(collect_references.call(value)) }
    refs
  when Array
    node.flat_map { |value| collect_references.call(value) }
  else
    []
  end
end
collect_references.call(doc).each do |ref|
  errors << "unresolved reference #{ref}" if resolve.call(ref).nil?
end

if errors.any?
  warn errors.map { |error| "#{path}: #{error}" }.join("\n")
  exit 1
end
puts "OpenAPI contract valid: #{operations.size} operations, local references resolved."
