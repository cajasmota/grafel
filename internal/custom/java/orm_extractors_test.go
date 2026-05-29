package java_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

	// Blank import to trigger init() registrations for custom_java_* extractors.
	_ "github.com/cajasmota/archigraph/internal/custom/java"
)

// ormFI builds a FileInput with the given source.
func ormFI(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

type ormEnt struct{ Kind, Subtype, Name string }

func runORM(t *testing.T, name string, file extreg.FileInput) []ormEnt {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	var out []ormEnt
	for _, ent := range ents {
		out = append(out, ormEnt{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name})
	}
	return out
}

func hasEnt(ents []ormEnt, kind, subtype, name string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Subtype == subtype && e.Name == name {
			return true
		}
	}
	return false
}

func hasSub(ents []ormEnt, subtype string) bool {
	for _, e := range ents {
		if e.Subtype == subtype {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// EclipseLink
// ---------------------------------------------------------------------------

func TestEclipseLinkEntityAndAssociation(t *testing.T) {
	src := `
import org.eclipse.persistence.annotations.Cache;

@Entity
@Table(name = "customers")
@Cache
public class Customer {
    @Id private Long id;

    @OneToMany(mappedBy = "customer")
    private List<Order> orders;

    @ManyToOne
    private Region region;
}
`
	ents := runORM(t, "custom_java_eclipselink", ormFI("Customer.java", "java", src))
	if !hasEnt(ents, "SCOPE.Schema", "entity", "Customer") {
		t.Errorf("expected Customer entity, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Component", "relation", "OneToMany:orders") {
		t.Errorf("expected OneToMany:orders relation, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Component", "relation", "ManyToOne:region") {
		t.Errorf("expected ManyToOne:region relation, got %v", ents)
	}
}

func TestEclipseLinkNamedQuery(t *testing.T) {
	src := `
// uses eclipselink provider
@Entity
@NamedQuery(name = "Customer.findAll", query = "SELECT c FROM Customer c")
public class Customer {}
`
	ents := runORM(t, "custom_java_eclipselink", ormFI("Customer.java", "java", src))
	if !hasEnt(ents, "SCOPE.Operation", "query", "Customer.findAll") {
		t.Errorf("expected named query Customer.findAll (verbatim name), got %v", ents)
	}
}

func TestEclipseLinkSkipsNonEclipseLink(t *testing.T) {
	// Plain JPA / Hibernate source with no EclipseLink marker -> no emission.
	src := `
@Entity
@Table(name = "users")
public class User { @Id private Long id; }
`
	ents := runORM(t, "custom_java_eclipselink", ormFI("User.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("expected no emission for non-eclipselink source, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Ebean
// ---------------------------------------------------------------------------

func TestEbeanModelAndFinder(t *testing.T) {
	src := `
import io.ebean.Finder;
import io.ebean.Model;

@Entity
@Table(name = "customers")
public class Customer extends Model {
    @Id Long id;

    @OneToMany
    List<Order> orders;

    public static final Finder<Long, Customer> find = new Finder<>(Customer.class);
}
`
	ents := runORM(t, "custom_java_ebean", ormFI("Customer.java", "java", src))
	if !hasEnt(ents, "SCOPE.Schema", "entity", "Customer") {
		t.Errorf("expected Customer entity, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Component", "finder", "finder:Customer") {
		t.Errorf("expected Finder:Customer, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Component", "relation", "OneToMany:orders") {
		t.Errorf("expected OneToMany:orders relation, got %v", ents)
	}
}

func TestEbeanQueryRoot(t *testing.T) {
	src := `
import io.ebean.DB;

public class CustomerService {
    public Customer get(Long id) {
        return DB.find(Customer.class).where().idEq(id).findOne();
    }
}
`
	ents := runORM(t, "custom_java_ebean", ormFI("CustomerService.java", "java", src))
	if !hasEnt(ents, "SCOPE.Operation", "query", "query:Customer") {
		t.Errorf("expected DB.find query root for Customer, got %v", ents)
	}
}

func TestEbeanSkipsNonEbean(t *testing.T) {
	src := `
@Entity
public class User { @Id Long id; }
`
	ents := runORM(t, "custom_java_ebean", ormFI("User.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("expected no emission for non-ebean source, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// MyBatis (annotation mode)
// ---------------------------------------------------------------------------

func TestMyBatisAnnotationMapper(t *testing.T) {
	src := `
import org.apache.ibatis.annotations.Mapper;
import org.apache.ibatis.annotations.Select;
import org.apache.ibatis.annotations.Insert;
import org.apache.ibatis.annotations.Results;

@Mapper
public interface CustomerMapper {

    @Select("SELECT id, name FROM customers WHERE id = #{id}")
    @Results(id = "customerMap", value = {})
    Customer findById(Long id);

    @Insert("INSERT INTO customers(name) VALUES(#{name})")
    int insert(Customer c);
}
`
	ents := runORM(t, "custom_java_mybatis", ormFI("CustomerMapper.java", "java", src))
	if !hasEnt(ents, "SCOPE.Component", "mapper", "CustomerMapper") {
		t.Errorf("expected CustomerMapper mapper, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Operation", "query", "CustomerMapper.findById") {
		t.Errorf("expected findById query, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Operation", "query", "CustomerMapper.insert") {
		t.Errorf("expected insert query, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Schema", "result_map", "customerMap") {
		t.Errorf("expected customerMap result map, got %v", ents)
	}
}

func TestMyBatisSkipsNonMyBatis(t *testing.T) {
	src := `public interface PlainService { String hello(); }`
	ents := runORM(t, "custom_java_mybatis", ormFI("PlainService.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("expected no emission for non-mybatis source, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// MyBatis (XML mapper mode) — implemented and unit-tested here; auto-dispatch
// of .xml files is future work (xml is not yet a classified language).
// ---------------------------------------------------------------------------

func TestMyBatisXMLMapper(t *testing.T) {
	src := `<?xml version="1.0" encoding="UTF-8" ?>
<!DOCTYPE mapper PUBLIC "-//mybatis.org//DTD Mapper 3.0//EN" "http://mybatis.org/dtd/mybatis-3-mapper.dtd">
<mapper namespace="com.app.CustomerMapper">
  <resultMap id="customerMap" type="com.app.Customer">
    <id property="id" column="id"/>
  </resultMap>
  <select id="findById" resultMap="customerMap">
    SELECT * FROM customers WHERE id = #{id}
  </select>
  <insert id="insert">
    INSERT INTO customers(name) VALUES(#{name})
  </insert>
</mapper>
`
	ents := runORM(t, "custom_java_mybatis", ormFI("CustomerMapper.xml", "xml", src))
	if !hasEnt(ents, "SCOPE.Component", "mapper", "com.app.CustomerMapper") {
		t.Errorf("expected namespace mapper, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Operation", "query", "com.app.CustomerMapper.findById") {
		t.Errorf("expected findById statement, got %v", ents)
	}
	if !hasSub(ents, "result_map") {
		t.Errorf("expected a result_map, got %v", ents)
	}
}
