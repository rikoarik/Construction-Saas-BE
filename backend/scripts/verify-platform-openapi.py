#!/usr/bin/env python3
"""Validate captured platform API JSON responses against api/openapi.yaml.

Usage: python3 backend/scripts/verify-platform-openapi.py /platform/dashboard response.json
Requires PyYAML and jsonschema; it never connects to a running service.
"""
import json
import pathlib
import sys

import jsonschema
import yaml


def main():
    if len(sys.argv) != 3:
        raise SystemExit("usage: verify-platform-openapi.py /platform/path response.json")
    contract = pathlib.Path(__file__).resolve().parents[2] / "api" / "openapi.yaml"
    with contract.open(encoding="utf-8") as source:
        document = yaml.safe_load(source)
    path = sys.argv[1]
    operation = document["paths"][path]["get"]
    response = operation["responses"]["200"]
    if "$ref" in response:
        response = document["components"]["responses"][response["$ref"].split("/")[-1]]
    schema = response["content"]["application/json"]["schema"]
    with open(sys.argv[2], encoding="utf-8") as source:
        payload = json.load(source)
    resolver = jsonschema.RefResolver.from_schema(document)
    jsonschema.Draft202012Validator(
        schema, resolver=resolver, format_checker=jsonschema.FormatChecker()
    ).validate(payload)
    print("response matches OpenAPI")


if __name__ == "__main__":
    main()
