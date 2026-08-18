package mcpserver

import (
	indexingapp "github.com/vtimame/istok.sh/internal/application/indexing"
	"github.com/vtimame/istok.sh/internal/codegraph"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/retrieval"
)

const indexToolSchemaVersion = "1"

type indexEmptyInput struct{}

type indexSearchInput struct {
	Query string `json:"query" jsonschema:"Lexical search query."`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum results to return."`
}

type graphSymbolInput struct {
	Name  string `json:"name" jsonschema:"Qualified or short symbol name."`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum symbols to return."`
}

type graphNeighborsInput struct {
	Name  string               `json:"name" jsonschema:"Qualified or short symbol name."`
	Kinds []codegraph.EdgeKind `json:"kinds,omitempty" jsonschema:"Optional relation kind filters."`
	Limit int                  `json:"limit,omitempty" jsonschema:"Maximum neighbors to return."`
}

type graphPathInput struct {
	From     string `json:"from" jsonschema:"Qualified or short source symbol name."`
	To       string `json:"to" jsonschema:"Qualified or short target symbol name."`
	MaxDepth int    `json:"max_depth,omitempty" jsonschema:"Maximum graph traversal depth."`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum paths to return."`
}

type indexStatusResult struct {
	SchemaVersion string              `json:"schema_version"`
	Project       *project.Project    `json:"project,omitempty"`
	Status        *indexingapp.Status `json:"status,omitempty"`
	Error         *ToolError          `json:"error,omitempty"`
}

type indexSearchResult struct {
	SchemaVersion   string                   `json:"schema_version"`
	Project         *project.Project         `json:"project,omitempty"`
	Status          *indexingapp.Status      `json:"status,omitempty"`
	ContractVersion string                   `json:"contract_version"`
	Results         []retrieval.SearchResult `json:"results"`
	Error           *ToolError               `json:"error,omitempty"`
}

type graphSymbolResult struct {
	SchemaVersion   string              `json:"schema_version"`
	Project         *project.Project    `json:"project,omitempty"`
	Status          *indexingapp.Status `json:"status,omitempty"`
	ContractVersion string              `json:"contract_version"`
	Symbols         []codegraph.Node    `json:"symbols"`
	Error           *ToolError          `json:"error,omitempty"`
}

type graphNeighborsResult struct {
	SchemaVersion   string                      `json:"schema_version"`
	Project         *project.Project            `json:"project,omitempty"`
	Status          *indexingapp.Status         `json:"status,omitempty"`
	ContractVersion string                      `json:"contract_version"`
	Neighbors       []indexingapp.GraphNeighbor `json:"neighbors"`
	Error           *ToolError                  `json:"error,omitempty"`
}

type graphPathResult struct {
	SchemaVersion   string                  `json:"schema_version"`
	Project         *project.Project        `json:"project,omitempty"`
	Status          *indexingapp.Status     `json:"status,omitempty"`
	ContractVersion string                  `json:"contract_version"`
	Paths           []indexingapp.GraphPath `json:"paths"`
	Error           *ToolError              `json:"error,omitempty"`
}
