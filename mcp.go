package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

// cw mcp is a Model Context Protocol server over stdio: it gives a Claude session (Skynet, the
// orchestrator) tools to see and drive every workspace. Messages are JSON-RPC 2.0, one per line.

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	run         func(args map[string]any) (any, error)
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const mcpProtocol = "2025-06-18"

// cmdMCP serves the tools until stdin closes: cw mcp.
func cmdMCP() error {
	tools := mcpTools()
	byName := map[string]mcpTool{}
	for _, t := range tools {
		byName[t.Name] = t
	}
	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	enc := json.NewEncoder(os.Stdout)
	reply := func(id json.RawMessage, result any, rerr *rpcError) {
		msg := map[string]any{"jsonrpc": "2.0", "id": id}
		if rerr != nil {
			msg["error"] = rerr
		} else {
			msg["result"] = result
		}
		_ = enc.Encode(msg)
	}
	for {
		line, err := in.ReadBytes('\n')
		if len(line) > 0 {
			var req rpcRequest
			if json.Unmarshal(line, &req) != nil {
				reply(json.RawMessage("null"), nil, &rpcError{-32700, "JSON inválido"})
			} else if len(req.ID) > 0 { // requests carry an id; notifications get no answer
				result, rerr := mcpHandle(req, tools, byName)
				reply(req.ID, result, rerr)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func mcpHandle(req rpcRequest, tools []mcpTool, byName map[string]mcpTool) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		v := p.ProtocolVersion
		if v == "" {
			v = mcpProtocol
		}
		return map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "cw", "version": version},
			"instructions": "Herramientas de cw (Claude Workspaces): ver y manejar los workspaces, sus pestañas, " +
				"vistas y sesiones de Claude o de terminal. Una sesión se nombra por su id o por su nombre; " +
				"si el nombre se repite, indica también el workspace o la pestaña.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{-32602, "parámetros inválidos"}
		}
		t, ok := byName[p.Name]
		if !ok {
			return nil, &rpcError{-32602, "herramienta desconocida: " + p.Name}
		}
		if p.Arguments == nil {
			p.Arguments = map[string]any{}
		}
		out, err := t.run(p.Arguments)
		if err != nil {
			return mcpText(err.Error(), true), nil
		}
		if s, ok := out.(string); ok {
			return mcpText(s, false), nil
		}
		data, _ := json.Marshal(out)
		return mcpText(string(data), false), nil
	}
	return nil, &rpcError{-32601, "método desconocido: " + req.Method}
}

func mcpText(s string, isErr bool) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": s}}, "isError": isErr}
}

// schema builds a JSON schema for a tool's arguments: props maps a name to "type:description".
func schema(required []string, props map[string][2]string) map[string]any {
	p := map[string]any{}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p[k] = map[string]any{"type": props[k][0], "description": props[k][1]}
	}
	s := map[string]any{"type": "object", "properties": p}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func argStr(a map[string]any, k string) string {
	switch v := a[k].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprint(int(v))
	}
	return ""
}

func argInt(a map[string]any, k string, def int) int {
	switch v := a[k].(type) {
	case float64:
		return int(v)
	case string:
		var n int
		if _, err := fmt.Sscan(v, &n); err == nil {
			return n
		}
	}
	return def
}

// mcpTools is every tool cw mcp offers.
func mcpTools() []mcpTool {
	return append(mcpReadTools(), mcpSessionTools()...)
}
