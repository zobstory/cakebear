package ir

// The URL Standard's URL and URLSearchParams, and AbortSignal.timeout (S1
// plan D6, D2): their operations' and properties' names.

var urlOpNames = map[HostOp]string{
	HostNewURL:             "new URL",
	HostURLCanParse:        "URL.canParse",
	HostURLParse:           "URL.parse",
	HostURLSet:             "a URL's setter",
	HostURLString:          "URL.toString",
	HostNewSearchParams:    "new URLSearchParams",
	HostParamsAppend:       "URLSearchParams.append",
	HostParamsDelete:       "URLSearchParams.delete",
	HostParamsGet:          "URLSearchParams.get",
	HostParamsGetAll:       "URLSearchParams.getAll",
	HostParamsHas:          "URLSearchParams.has",
	HostParamsSet:          "URLSearchParams.set",
	HostParamsSort:         "URLSearchParams.sort",
	HostParamsString:       "URLSearchParams.toString",
	HostParamsForEach:      "URLSearchParams.forEach",
	HostAbortSignalTimeout: "AbortSignal.timeout",
	HostEnvGet:             "process.env",
	HostEnvHas:             "in process.env",
}

// URLProps maps a URL's getters, by name, to their properties: every one a
// string but searchParams.
var URLProps = map[string]HostProperty{
	"href": HostURLHref, "origin": HostURLOrigin, "protocol": HostURLProtocol,
	"username": HostURLUsername, "password": HostURLPassword, "host": HostURLHost,
	"hostname": HostURLHostname, "port": HostURLPort, "pathname": HostURLPathname,
	"search": HostURLSearch, "hash": HostURLHash, "searchParams": HostURLSearchParams,
}

var urlPropNames = func() map[HostProperty]string {
	m := map[HostProperty]string{HostParamsSize: "URLSearchParams.size"}
	for name, p := range URLProps {
		m[p] = "URL." + name
	}
	return m
}()
