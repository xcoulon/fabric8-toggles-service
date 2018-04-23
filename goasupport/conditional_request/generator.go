package conditionalrequest

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"strings"

	"github.com/goadesign/goa/design"
	"github.com/goadesign/goa/goagen/codegen"
)

// Generate adds method to support conditional queries
func Generate() ([]string, error) {
	var (
		ver    string
		outDir string
	)
	set := flag.NewFlagSet("app", flag.PanicOnError)
	set.String("design", "", "") // Consume design argument so Parse doesn't complain
	set.StringVar(&ver, "version", "", "")
	set.StringVar(&outDir, "out", "", "")
	set.Parse(os.Args[2:])

	// First check compatibility
	if err := codegen.CheckVersion(ver); err != nil {
		return nil, err
	}

	return WriteNames(design.Design, outDir)
}

// RequestContext holds a single goa Request Context object
type RequestContext struct {
	Name   string
	Entity Entity
}

// RequestHeader holds a single HTTP Header as defined in the design for a Request Context
type RequestHeader struct {
	Name   string
	Header string
	Type   string
}

// Entity holds a single goa Response entity object that can be used in multiple responses.
type Entity struct {
	AppTypeName    string
	DomainTypeName string
	IsSingle       bool
	IsList         bool
}

func contains(entities []Entity, entity Entity) bool {
	for _, e := range entities {
		if e.AppTypeName == entity.AppTypeName {
			return true
		}
	}
	return false

}

// aliases for the domain model packages, to avoid conflict with structure names generated in the `app` package
var packageAliases map[string]string

// map of domain structure names and their corresponding aliased package (unknown at the design level)
var structPackages map[string]string

var ignoredStructs []string

func init() {
	structPackages = make(map[string]string)
	structPackages["UserFeature"] = "featuretoggles"
}

// WriteNames creates the names.txt file.
func WriteNames(api *design.APIDefinition, outDir string) ([]string, error) {
	// Now iterate through the resources to gather their names
	var contexts []RequestContext
	var entities []Entity

	api.IterateResources(func(res *design.ResourceDefinition) error {
		res.IterateActions(func(act *design.ActionDefinition) error {
			name := fmt.Sprintf("%v%vContext", codegen.Goify(act.Name, true), codegen.Goify(res.Name, true))
			// look-up headers for conditional request support
			if act.Headers != nil {
				// look-up headers and entity types in responses
				if act.Responses != nil {
					for _, response := range act.Responses {
						if response.Name == design.OK && response.Type != nil {
							if mt, ok := response.Type.(*design.MediaTypeDefinition); ok {
								var entity *Entity
								// lookup conditional request/response headers
								for header := range response.Headers.Type.ToObject() {
									if header == "ETag" {
										// assume that a "list" entities have their name ending with "List"
										// and "single" entities have their name ending with "Single"
										isList := strings.HasSuffix(mt.TypeName, "List")
										isArray := strings.HasSuffix(mt.TypeName, "Array")
										var domainTypeName string
										if isList {
											domainTypeName = strings.TrimSuffix(mt.TypeName, "List")
										} else if isArray {
											domainTypeName = strings.TrimSuffix(mt.TypeName, "Array")
										} else {
											domainTypeName = strings.TrimSuffix(mt.TypeName, "Single")
										}
										// prepend the package
										domainTypeName = fmt.Sprintf("%s.%s", structPackages[domainTypeName], domainTypeName)
										entity = &Entity{
											AppTypeName:    mt.TypeName,
											DomainTypeName: domainTypeName,
											IsList:         isList || isArray,
											IsSingle:       !(isList || isArray),
										}
										break
									}
								}
								// skip if no response header was found
								if entity != nil {
									fmt.Printf("Response context: %s -> entity: %v\n", name, entity)
									// for k, v := range m.ToObject() {
									// 	fmt.Printf("%s -> %v\n", k, v)
									// }
									ctx := RequestContext{Name: name, Entity: *entity}
									contexts = append(contexts, ctx)
									if !contains(entities, *entity) {
										entities = append(entities, *entity)
									}

								}
							}
						}
					}
				}
			}
			return nil
		})
		return nil
	})

	ctxFile := filepath.Join(outDir, "conditional_requests.go")
	ctxWr, err := codegen.SourceFileFor(ctxFile)
	if err != nil {
		panic(err) // bug
	}
	title := fmt.Sprintf("%s: Conditional Requests methods - See goasupport/conditional_request/generator.go", api.Context())
	imports := []*codegen.ImportSpec{
		codegen.SimpleImport("bytes"),
		codegen.SimpleImport("crypto/md5"),
		codegen.SimpleImport("encoding/base64"),
		codegen.SimpleImport("strconv"),
		codegen.SimpleImport("net/http"),
		codegen.SimpleImport("time"),
		codegen.SimpleImport("fmt"),
		codegen.SimpleImport("reflect"),
		codegen.SimpleImport("github.com/fabric8-services/fabric8-toggles-service/configuration"),
		codegen.SimpleImport("github.com/fabric8-services/fabric8-auth/log"),
		codegen.SimpleImport("github.com/fabric8-services/fabric8-toggles-service/featuretoggles"),
		codegen.NewImport("uuid", "github.com/satori/go.uuid"),
	}
	// add imports for domain packages
	for alias, pkg := range packageAliases {
		imports = append(imports, codegen.NewImport(alias, pkg))
	}
	ctxWr.WriteHeader(title, "app", imports)
	if err := ctxWr.ExecuteTemplate("constants", constants, nil, nil); err != nil {
		return nil, err
	}
	if err := ctxWr.ExecuteTemplate("cacheControlConfig", cacheControlConfig, nil, nil); err != nil {
		return nil, err
	}
	if err := ctxWr.ExecuteTemplate("conditionalRequestContext", conditionalRequestContext, nil, nil); err != nil {
		return nil, err
	}
	if err := ctxWr.ExecuteTemplate("conditionalResponseEntity", conditionalResponseEntity, nil, nil); err != nil {
		return nil, err
	}
	if err := ctxWr.ExecuteTemplate("doConditionals", doConditionals, nil, nil); err != nil {
		return nil, err
	}
	if err := ctxWr.ExecuteTemplate("generateETag", generateETag, nil, nil); err != nil {
		return nil, err
	}
	if err := ctxWr.ExecuteTemplate("matchesETag", matchesETag, nil, nil); err != nil {
		return nil, err
	}
	for _, ctx := range contexts {
		if err := ctxWr.ExecuteTemplate("conditional", conditional, nil, ctx); err != nil {
			return nil, err
		}
		if err := ctxWr.ExecuteTemplate("getIfNoneMatch", getIfNoneMatch, nil, ctx); err != nil {
			return nil, err
		}
		if err := ctxWr.ExecuteTemplate("setETag", setETag, nil, ctx); err != nil {
			return nil, err
		}
		if err := ctxWr.ExecuteTemplate("setCacheControl", setCacheControl, nil, ctx); err != nil {
			return nil, err
		}
	}
	err = ctxWr.FormatCode()
	if err != nil {
		return nil, err
	}
	return []string{ctxFile}, nil
}

const (
	constants = `
	const (
	// IfNoneMatch the "If-None-Match" HTTP request header name
	IfNoneMatch = "If-None-Match"
	// ETag the "ETag" HTTP response header name
	// should be ETag but GOA will convert it to "Etag" when setting the header.
	// Plus, RFC 2616 specifies that header names are case insensitive:
	// https://www.w3.org/Protocols/rfc2616/rfc2616-sec4.html#sec4.2
	ETag = "Etag"
	// CacheControl the "Cache-Control" HTTP response header name
	CacheControl = "Cache-Control"
	// MaxAge the "max-age" HTTP response header value
	MaxAge = "max-age"
)`

	conditionalRequestContext = `
// ConditionalRequestContext interface with methods for the contexts
type ConditionalRequestContext interface {
	NotModified() error
	getIfNoneMatch() *string
	setETag(string)
	setCacheControl(string)
}`

	conditionalResponseEntity = `
	// ConditionalRequestEntity interface with methods for the response entities
type ConditionalRequestEntity interface {
	// returns the values to use to generate the ETag
	GetETagData() []interface{}
}`

	cacheControlConfig = `
   type CacheControlConfig func() string
   `
	doConditionals = `
func doConditionalRequest(ctx ConditionalRequestContext, entity ConditionalRequestEntity, cacheControlConfig CacheControlConfig, nonConditionalCallback func() error) error {
	eTag := GenerateEntityTag(entity)
	cacheControl := cacheControlConfig()
	ctx.setETag(eTag)
	ctx.setCacheControl(cacheControl)
	// check the 'If-None-Match' header first.
	found, match := matchesETag(ctx, eTag)
	if found && match {
		return ctx.NotModified()
	}
	// call the 'nonConditionalCallback' if the entity was modified since the client's last call
	return nonConditionalCallback()
}

func doConditionalEntities(ctx ConditionalRequestContext, entities []ConditionalRequestEntity, cacheControlConfig CacheControlConfig, nonConditionalCallback func() error) error {
	var eTag string
	if len(entities) > 0 {
		eTag = GenerateEntitiesTag(entities)
	} else {
		eTag = GenerateEmptyTag()
	}
	ctx.setETag(eTag)
	cacheControl := cacheControlConfig()
	ctx.setCacheControl(cacheControl)
	// check the 'If-None-Match' header first.
	found, match := matchesETag(ctx, eTag)
	if found && match {
		return ctx.NotModified()
	}
	// call the 'nonConditionalCallback' if the entity was modified since the client's last call
	return nonConditionalCallback()
}`

	conditional = `
{{ $resp := . }}
{{ $entity := $resp.Entity }}
{{ if $entity.IsSingle }}
// ConditionalRequest checks if the entity to return changed since the client's last call and returns a "304 Not Modified" response
// or calls the 'nonConditionalCallback' function to carry on.
func (ctx *{{$resp.Name}}) ConditionalRequest(entity {{$entity.DomainTypeName}}, cacheControlConfig CacheControlConfig, nonConditionalCallback func() error) error {
	return doConditionalRequest(ctx, entity, cacheControlConfig, nonConditionalCallback)
}

{{ end }}
{{ if $entity.IsList }}
// ConditionalEntities checks if the entities to return changed since the client's last call and returns a "304 Not Modified" response
// or calls the 'nonConditionalCallback' function to carry on.
func (ctx *{{$resp.Name}}) ConditionalEntities(entities []{{$entity.DomainTypeName}}, cacheControlConfig CacheControlConfig, nonConditionalCallback func() error) error {
	conditionalEntities := make([]ConditionalRequestEntity, len(entities))
	for i, entity := range entities {
		conditionalEntities[i] = entity
	}
	return doConditionalEntities(ctx, conditionalEntities, cacheControlConfig, nonConditionalCallback)
}

{{ end }}`
	generateETag = `
// GenerateEmptyTag generates the value to return in the "ETag" HTTP response header for the an empty list of entities
// The ETag is the base64-encoded value of the md5 hash of the buffer content
func GenerateEmptyTag() string {
	var buffer bytes.Buffer
	buffer.WriteString("empty")
	etagData := md5.Sum(buffer.Bytes())
	etag := base64.StdEncoding.EncodeToString(etagData[:])
	return etag
}
// GenerateEntityTag generates the value to return in the "ETag" HTTP response header for the given entity
// The ETag is the base64-encoded value of the md5 hash of the buffer content
func GenerateEntityTag(entity ConditionalRequestEntity) string {
	var buffer bytes.Buffer
	buffer.WriteString(generateETagValue(entity.GetETagData()))
	etagData := md5.Sum(buffer.Bytes())
	etag := base64.StdEncoding.EncodeToString(etagData[:])
	return etag
}

// GenerateEntitiesTag generates the value to return in the "ETag" HTTP response header for the given list of entities
// The ETag is the base64-encoded value of the md5 hash of the buffer content
func GenerateEntitiesTag(entities []ConditionalRequestEntity) string {
	var buffer bytes.Buffer
	for i, entity := range entities {
		buffer.WriteString(generateETagValue(entity.GetETagData()))
		if i < len(entities)-1 {
			buffer.WriteString("\n")
		}
	}
	etagData := md5.Sum(buffer.Bytes())
	etag := base64.StdEncoding.EncodeToString(etagData[:])
	return etag
}
func generateETagValue(data []interface{}, options ...interface{}) string {
	var buffer bytes.Buffer
	for i, d := range data {
		switch d := d.(type) {
		case []interface{}:
			// if the entry in the 'data' array is itself an array,
			// then we recursively call the 'generateETagValue' function with this array entry.
			buffer.WriteString(generateETagValue(d))
		case string:
			buffer.WriteString(d)
		case *string:
			if d != nil {
				buffer.WriteString(*d)
			}
		case time.Time:
			buffer.WriteString(d.UTC().String())
		case *time.Time:
			if d != nil {
				buffer.WriteString(d.UTC().String())
			}
		case int:
			buffer.WriteString(strconv.Itoa(d))
		case *int:
			if d != nil {
				buffer.WriteString(strconv.Itoa(*d))
			}
		case uuid.UUID:
			buffer.WriteString(d.String())
		case *uuid.UUID:
			if d != nil {
				buffer.WriteString(d.String())
			}
		default:
			log.Logger().Errorln(fmt.Sprintf("Unexpected Etag fragment format: %v", reflect.TypeOf(d)))
		}
		if i < len(data)-1 {
			buffer.WriteString("|")
		}
	}
	return buffer.String()
}`

	setETag = `
{{ $resp := . }}
// setETag sets the 'ETag' header
func (ctx *{{$resp.Name}}) setETag(value string) {
	ctx.ResponseData.Header().Set(ETag, value)
}`

	getIfNoneMatch = `
{{ $resp := . }}
// getIfNoneMatch sets the 'If-None-Match' header
func (ctx *{{$resp.Name}}) getIfNoneMatch() *string {
	return ctx.IfNoneMatch
}`

	matchesETag = `
// matchesETag compares the given 'etag' argument matches with the context's 'IfNoneMatch' value.
// Returns 'true, true' if the 'If-None-Match' field was found and matched given 'etag' argument
// Returns 'true, false' if the 'If-None-Match' field was found but did not match given 'etag' argument
// Returns 'false, false' if the 'If-None-Match' field was not found
func matchesETag(ctx ConditionalRequestContext, etag string) (bool, bool) {
	if ctx.getIfNoneMatch() != nil {
		if *ctx.getIfNoneMatch() == etag {
			// 'If-None-Match' field was found and matched the given 'etag' argument
			return true, true
		}
		// 'If-None-Match' field was found and but did not match the given 'etag' argument
		return true, false
	}
	// 'If-None-Match' field was not found
	return false, false
}`
	setCacheControl = `
{{ $resp := . }}
// SetCacheControl sets the 'Cache-Control' header
func (ctx *{{$resp.Name}}) setCacheControl(value string) {
	ctx.ResponseData.Header().Set(CacheControl, value)
}`
	toHTTPTime = `
// ToHTTPTime utility function to convert a 'time.Time' into a valid HTTP date
func ToHTTPTime(value time.Time) string {
	return value.UTC().Format(http.TimeFormat)
}`
)