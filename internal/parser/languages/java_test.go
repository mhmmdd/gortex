package languages

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzet/gortex/internal/graph"
)

func TestJavaExtractor_Class(t *testing.T) {
	src := []byte(`public class UserService {
    public User getUser(String id) {
        return null;
    }
}
`)
	e := NewJavaExtractor()
	result, err := e.Extract("UserService.java", src)
	require.NoError(t, err)

	types := nodesOfKind(result.Nodes, graph.KindType)
	require.Len(t, types, 1)
	assert.Equal(t, "UserService", types[0].Name)

	methods := nodesOfKind(result.Nodes, graph.KindMethod)
	require.Len(t, methods, 1)
	assert.Equal(t, "getUser", methods[0].Name)
}

func TestJavaExtractor_Interface(t *testing.T) {
	src := []byte(`public interface Repository {
    User findById(String id);
    void save(User user);
}
`)
	e := NewJavaExtractor()
	result, err := e.Extract("Repository.java", src)
	require.NoError(t, err)

	ifaces := nodesOfKind(result.Nodes, graph.KindInterface)
	require.Len(t, ifaces, 1)
	assert.Equal(t, "Repository", ifaces[0].Name)
}

func TestJavaExtractor_Imports(t *testing.T) {
	src := []byte(`import java.util.List;
import com.example.service.UserService;

public class App {}
`)
	e := NewJavaExtractor()
	result, err := e.Extract("App.java", src)
	require.NoError(t, err)

	imports := edgesOfKind(result.Edges, graph.EdgeImports)
	require.Len(t, imports, 2)
}
