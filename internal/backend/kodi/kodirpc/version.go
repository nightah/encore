package kodirpc

import "fmt"

// Less reports whether v is older than w.
func (v APIVersion) Less(w APIVersion) bool {
	if v.Major != w.Major {
		return v.Major < w.Major
	}
	if v.Minor != w.Minor {
		return v.Minor < w.Minor
	}
	return v.Patch < w.Patch
}

func (v APIVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// AvailableIn reports whether a server speaking API version v provides the method.
func (m MethodInfo) AvailableIn(v APIVersion) bool {
	return !v.Less(m.Since)
}

// ParamAvailableIn reports whether a server speaking API version v accepts the named parameter.
func (m MethodInfo) ParamAvailableIn(param string, v APIVersion) bool {
	if since, ok := m.ParamsSince[param]; ok {
		return !v.Less(since)
	}
	return m.AvailableIn(v)
}
