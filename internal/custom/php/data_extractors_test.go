package php_test

// data_extractors_test.go — tests for orm_data.go, driver_sql.go, test_data.go
// Coverage: Doctrine, Eloquent, CycleORM, Propel, RedBeanPHP schema/rel/migration;
// MySQL/Postgres/SQLite driver schema; Behat/Codeception/Pest test extractors.

import "testing"

// ============================================================================
// Doctrine ORM
// ============================================================================

func TestDoctrineColumn(t *testing.T) {
	src := `<?php
namespace App\Entity;
use Doctrine\ORM\Mapping as ORM;

#[ORM\Entity(repositoryClass: UserRepository::class)]
class User
{
    #[ORM\Column(type: 'string', length: 255)]
    private string $username;

    #[ORM\Column(type: 'string', unique: true)]
    private string $email;
}
`
	ents := extract(t, "php_doctrine_orm_data", fi("User.php", "php", src))
	if !containsEntity(ents, "SCOPE.Schema", "username") {
		t.Error("expected username column schema")
	}
	if !containsEntity(ents, "SCOPE.Schema", "email") {
		t.Error("expected email column schema")
	}
}

func TestDoctrineRelationship(t *testing.T) {
	src := `<?php
#[ORM\Entity]
class Order
{
    #[ORM\ManyToOne(targetEntity: Customer::class, inversedBy: 'orders')]
    private Customer $customer;

    #[ORM\OneToMany(targetEntity: OrderItem::class, mappedBy: 'order')]
    private Collection $items;
}
`
	ents := extract(t, "php_doctrine_orm_data", fi("Order.php", "php", src))
	if !containsEntity(ents, "SCOPE.Component", "relation:ManyToOne") {
		t.Error("expected ManyToOne relation component")
	}
	if !containsEntity(ents, "SCOPE.Component", "relation:OneToMany") {
		t.Error("expected OneToMany relation component")
	}
}

func TestDoctrineFK(t *testing.T) {
	src := `<?php
#[ORM\Entity]
class OrderItem
{
    #[ORM\ManyToOne]
    #[ORM\JoinColumn(nullable: false)]
    private Order $order;
}
`
	ents := extract(t, "php_doctrine_orm_data", fi("OrderItem.php", "php", src))
	if !containsEntity(ents, "SCOPE.Schema", "join_column") {
		t.Error("expected join_column FK entity")
	}
}

func TestDoctrineLazyLoading(t *testing.T) {
	src := `<?php
#[ORM\Entity]
class Product
{
    #[ORM\OneToMany(fetch: 'LAZY', targetEntity: Review::class)]
    private Collection $reviews;
}
`
	ents := extract(t, "php_doctrine_orm_data", fi("Product.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "fetch:LAZY") {
		t.Error("expected fetch:LAZY pattern entity")
	}
}

func TestDoctrineMigration(t *testing.T) {
	src := `<?php
use Doctrine\Migrations\AbstractMigration;

class Version20230101120000 extends AbstractMigration
{
    public function up(Schema $schema): void
    {
        $this->addSql('CREATE TABLE user (id INT NOT NULL)');
    }

    public function down(Schema $schema): void
    {
        $this->addSql('DROP TABLE user');
    }
}
`
	ents := extract(t, "php_doctrine_orm_data", fi("Version20230101120000.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "Version20230101120000") {
		t.Error("expected migration class entity")
	}
	if !containsEntity(ents, "SCOPE.Operation", "migration:up") {
		t.Error("expected migration:up entity")
	}
	if !containsEntity(ents, "SCOPE.Operation", "migration:down") {
		t.Error("expected migration:down entity")
	}
}

func TestDoctrineNoMatch(t *testing.T) {
	src := `<?php echo "hello doctrine";`
	ents := extract(t, "php_doctrine_orm_data", fi("plain.php", "php", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ============================================================================
// Doctrine deep extraction — entity classes, column types, targetEntity,
// JoinColumn FK fields, EAGER/EXTRA_LAZY fetch, addSql migration SQL.
// ============================================================================

// TestDoctrineDeep_EntityClassAndColumnTypes verifies that the #[ORM\Entity]
// attribute causes the class name to be emitted and that column type args are
// captured on each property.
func TestDoctrineDeep_EntityClassAndColumnTypes(t *testing.T) {
	src := `<?php
namespace App\Entity;
use Doctrine\ORM\Mapping as ORM;

#[ORM\Entity(repositoryClass: UserRepository::class)]
class User
{
    #[ORM\Id]
    #[ORM\Column(type: 'integer')]
    private int $id;

    #[ORM\Column(type: 'string', length: 255)]
    private string $username;

    #[ORM\Column(type: 'string', unique: true)]
    private string $email;

    #[ORM\Column(type: 'boolean')]
    private bool $active;
}
`
	ents := extract(t, "php_doctrine_orm_data", fi("User.php", "php", src))

	// Entity class name must be emitted as SCOPE.Schema/entity.
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User entity class (SCOPE.Schema/entity)")
	}

	// Each annotated property must appear as SCOPE.Schema/column.
	for _, col := range []string{"id", "username", "email", "active"} {
		if !containsEntity(ents, "SCOPE.Schema", col) {
			t.Errorf("expected column entity %q", col)
		}
	}
}

// TestDoctrineDeep_AnnotationStyleEntityAndColumns verifies that the legacy
// @ORM\Entity / @ORM\Column annotation syntax (Doctrine 2 / Symfony 4–5 style)
// is handled identically to PHP8 attributes.
func TestDoctrineDeep_AnnotationStyleEntityAndColumns(t *testing.T) {
	src := `<?php
/**
 * @ORM\Entity(repositoryClass="App\Repository\PostRepository")
 */
class Post
{
    /**
     * @ORM\Column(type="string", length=255)
     */
    private $title;

    /**
     * @ORM\Column(type="text")
     */
    private $body;
}
`
	ents := extract(t, "php_doctrine_orm_data", fi("Post.php", "php", src))

	if !containsEntity(ents, "SCOPE.Schema", "title") {
		t.Error("expected title column from @ORM\\Column annotation")
	}
	if !containsEntity(ents, "SCOPE.Schema", "body") {
		t.Error("expected body column from @ORM\\Column annotation")
	}
}

// TestDoctrineDeep_AssociationWithTargetEntity verifies that all four relation
// types are emitted with the target_entity property extracted from the attribute.
func TestDoctrineDeep_AssociationWithTargetEntity(t *testing.T) {
	src := `<?php
#[ORM\Entity]
class Post
{
    #[ORM\OneToMany(targetEntity: Comment::class, mappedBy: 'post', fetch: 'LAZY')]
    private Collection $comments;

    #[ORM\ManyToOne(targetEntity: User::class, inversedBy: 'posts')]
    private User $author;

    #[ORM\ManyToMany(targetEntity: Tag::class)]
    private Collection $tags;

    #[ORM\OneToOne(targetEntity: Profile::class, cascade: ['persist'])]
    private ?Profile $profile;
}
`
	ents := extract(t, "php_doctrine_orm_data", fi("Post.php", "php", src))

	// All four relation types must produce SCOPE.Component/relation entities.
	for _, relType := range []string{"OneToMany", "ManyToOne", "ManyToMany", "OneToOne"} {
		if !containsEntity(ents, "SCOPE.Component", "relation:"+relType) {
			t.Errorf("expected relation:%s component", relType)
		}
	}
}

// TestDoctrineDeep_JoinColumnExactFields verifies that name= and
// referencedColumnName= values are captured from #[ORM\JoinColumn].
func TestDoctrineDeep_JoinColumnExactFields(t *testing.T) {
	src := `<?php
#[ORM\Entity]
class Order
{
    #[ORM\ManyToOne(targetEntity: Customer::class)]
    #[ORM\JoinColumn(name: 'customer_id', referencedColumnName: 'id')]
    private Customer $customer;

    #[ORM\ManyToOne(targetEntity: Address::class)]
    #[ORM\JoinColumn(name: 'shipping_address_id', referencedColumnName: 'id', nullable: true)]
    private ?Address $shippingAddress;
}
`
	ents := extract(t, "php_doctrine_orm_data", fi("Order.php", "php", src))

	// FK entities are named "fk:<column_name>" when name= is present.
	if !containsEntity(ents, "SCOPE.Schema", "fk:customer_id") {
		t.Error("expected fk:customer_id foreign_key entity from JoinColumn name=")
	}
	if !containsEntity(ents, "SCOPE.Schema", "fk:shipping_address_id") {
		t.Error("expected fk:shipping_address_id foreign_key entity from JoinColumn name=")
	}
}

// TestDoctrineDeep_JoinTable verifies that #[ORM\JoinTable] emits a
// foreign_key entity for many-to-many pivot table definitions.
func TestDoctrineDeep_JoinTable(t *testing.T) {
	src := `<?php
#[ORM\Entity]
class Article
{
    #[ORM\ManyToMany(targetEntity: Tag::class)]
    #[ORM\JoinTable(name: 'article_tags')]
    private Collection $tags;
}
`
	ents := extract(t, "php_doctrine_orm_data", fi("Article.php", "php", src))

	if !containsEntity(ents, "SCOPE.Schema", "join_table") {
		t.Error("expected join_table foreign_key entity from #[ORM\\JoinTable]")
	}
}

// TestDoctrineDeep_FetchEager verifies that fetch: 'EAGER' is detected as
// a lazy_loading pattern entity with fetch_mode=EAGER.
func TestDoctrineDeep_FetchEager(t *testing.T) {
	src := `<?php
#[ORM\Entity]
class Invoice
{
    #[ORM\OneToMany(targetEntity: InvoiceLine::class, mappedBy: 'invoice', fetch: 'EAGER')]
    private Collection $lines;

    #[ORM\ManyToMany(targetEntity: Tag::class, fetch: 'EXTRA_LAZY')]
    private Collection $tags;
}
`
	ents := extract(t, "php_doctrine_orm_data", fi("Invoice.php", "php", src))

	if !containsEntity(ents, "SCOPE.Pattern", "fetch:EAGER") {
		t.Error("expected fetch:EAGER lazy_loading pattern entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "fetch:EXTRA_LAZY") {
		t.Error("expected fetch:EXTRA_LAZY lazy_loading pattern entity")
	}
}

// TestDoctrineDeep_MigrationAddSqlContent verifies that $this->addSql(...)
// calls in Doctrine migrations are extracted with the SQL string as an entity.
func TestDoctrineDeep_MigrationAddSqlContent(t *testing.T) {
	src := `<?php
use Doctrine\Migrations\AbstractMigration;
use Doctrine\DBAL\Schema\Schema;

class Version20240115000000 extends AbstractMigration
{
    public function up(Schema $schema): void
    {
        $this->addSql('CREATE TABLE orders (id INT NOT NULL, customer_id INT NOT NULL)');
        $this->addSql('ALTER TABLE orders ADD CONSTRAINT fk_orders_customer FOREIGN KEY (customer_id) REFERENCES customers (id)');
    }

    public function down(Schema $schema): void
    {
        $this->addSql('DROP TABLE orders');
    }
}
`
	ents := extract(t, "php_doctrine_orm_data", fi("Version20240115000000.php", "php", src))

	// Migration class and methods.
	if !containsEntity(ents, "SCOPE.Operation", "Version20240115000000") {
		t.Error("expected Version20240115000000 migration class entity")
	}
	if !containsEntity(ents, "SCOPE.Operation", "migration:up") {
		t.Error("expected migration:up step entity")
	}
	if !containsEntity(ents, "SCOPE.Operation", "migration:down") {
		t.Error("expected migration:down step entity")
	}

	// SQL content entities: prefix "sql:" + first 64 chars of the SQL string.
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "migration_sql" &&
			len(e.Name) > 4 && e.Name[:4] == "sql:" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one migration_sql entity from $this->addSql(...)")
	}
}

// TestDoctrineDeep_FullEntityFile exercises a realistic entity file combining
// entity class, multiple column types, associations with targetEntity,
// JoinColumn FK with name+referencedColumnName, and fetch modes — all in one
// pass.  This is the integration test for the "TS/JS bar" requirement.
func TestDoctrineDeep_FullEntityFile(t *testing.T) {
	src := `<?php
namespace App\Entity;

use Doctrine\ORM\Mapping as ORM;
use Doctrine\Common\Collections\Collection;
use Doctrine\Common\Collections\ArrayCollection;

#[ORM\Entity(repositoryClass: OrderRepository::class)]
class Order
{
    #[ORM\Id]
    #[ORM\Column(type: 'integer')]
    private int $id;

    #[ORM\Column(type: 'string', length: 50)]
    private string $status;

    #[ORM\Column(type: 'decimal', precision: 10, scale: 2)]
    private string $total;

    #[ORM\ManyToOne(targetEntity: Customer::class, inversedBy: 'orders')]
    #[ORM\JoinColumn(name: 'customer_id', referencedColumnName: 'id', nullable: false)]
    private Customer $customer;

    #[ORM\OneToMany(targetEntity: OrderItem::class, mappedBy: 'order', fetch: 'LAZY', cascade: ['persist'])]
    private Collection $items;

    #[ORM\ManyToMany(targetEntity: Coupon::class, fetch: 'EXTRA_LAZY')]
    #[ORM\JoinTable(name: 'order_coupons')]
    private Collection $coupons;
}
`
	ents := extract(t, "php_doctrine_orm_data", fi("Order.php", "php", src))

	// Entity class.
	if !containsEntity(ents, "SCOPE.Schema", "Order") {
		t.Error("expected Order entity class")
	}

	// Columns.
	for _, col := range []string{"id", "status", "total"} {
		if !containsEntity(ents, "SCOPE.Schema", col) {
			t.Errorf("expected column %q", col)
		}
	}

	// Relation types.
	for _, rt := range []string{"ManyToOne", "OneToMany", "ManyToMany"} {
		if !containsEntity(ents, "SCOPE.Component", "relation:"+rt) {
			t.Errorf("expected relation:%s", rt)
		}
	}

	// JoinColumn FK with exact name.
	if !containsEntity(ents, "SCOPE.Schema", "fk:customer_id") {
		t.Error("expected fk:customer_id from JoinColumn name=customer_id")
	}

	// JoinTable FK.
	if !containsEntity(ents, "SCOPE.Schema", "join_table") {
		t.Error("expected join_table from #[ORM\\JoinTable]")
	}

	// Fetch modes.
	if !containsEntity(ents, "SCOPE.Pattern", "fetch:LAZY") {
		t.Error("expected fetch:LAZY pattern")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "fetch:EXTRA_LAZY") {
		t.Error("expected fetch:EXTRA_LAZY pattern")
	}
}

// ============================================================================
// Eloquent ORM
// ============================================================================

func TestEloquentFillableSchema(t *testing.T) {
	src := `<?php
use Illuminate\Database\Eloquent\Model;

class Post extends Model
{
    protected $fillable = ['title', 'content', 'published_at'];
    protected $casts = [
        'published_at' => 'datetime',
        'is_active' => 'boolean',
    ];
}
`
	ents := extract(t, "php_eloquent_orm_data", fi("Post.php", "php", src))
	if !containsEntity(ents, "SCOPE.Schema", "title") {
		t.Error("expected title column schema from fillable")
	}
	if !containsEntity(ents, "SCOPE.Schema", "published_at") {
		t.Error("expected published_at column from fillable OR casts")
	}
}

func TestEloquentBelongsToFK(t *testing.T) {
	src := `<?php
class Comment extends Model
{
    public function post(): BelongsTo
    {
        return $this->belongsTo(Post::class);
    }
    public function tags(): BelongsToMany
    {
        return $this->belongsToMany(Tag::class);
    }
}
`
	ents := extract(t, "php_eloquent_orm_data", fi("Comment.php", "php", src))
	if !containsEntity(ents, "SCOPE.Component", "post") {
		t.Error("expected post belongsTo relation")
	}
	if !containsEntity(ents, "SCOPE.Component", "tags") {
		t.Error("expected tags belongsToMany relation")
	}
}

func TestEloquentMigration(t *testing.T) {
	src := `<?php
use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

class CreatePostsTable extends Migration
{
    public function up()
    {
        Schema::create('posts', function (Blueprint $table) {
            $table->id();
            $table->string('title');
            $table->text('content');
            $table->foreign('user_id')->references('id')->on('users');
            $table->timestamps();
        });
    }
}
`
	ents := extract(t, "php_eloquent_orm_data", fi("2023_01_01_create_posts_table.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "create:posts") {
		t.Error("expected create:posts migration entity")
	}
	if !containsEntity(ents, "SCOPE.Schema", "title") {
		t.Error("expected title column schema from blueprint")
	}
	if !containsEntity(ents, "SCOPE.Schema", "fk:user_id") {
		t.Error("expected fk:user_id foreign key entity")
	}
}

func TestEloquentEagerLoading(t *testing.T) {
	src := `<?php
class User extends Model
{
    protected $with = ['profile', 'roles'];
}
`
	ents := extract(t, "php_eloquent_orm_data", fi("User.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "eager_with") {
		t.Error("expected eager_with lazy/eager loading pattern")
	}
}

// ============================================================================
// Eloquent deep extraction — models, relationships, FK, lazy, migrations
// ============================================================================

// TestEloquentDeep_ModelExtraction verifies that model class name, $table override,
// $fillable columns, and $casts entries are all surfaced.
func TestEloquentDeep_ModelExtraction(t *testing.T) {
	src := `<?php
namespace App\Models;

use Illuminate\Database\Eloquent\Model;

class User extends Model
{
    protected $table = 'app_users';
    protected $fillable = ['name', 'email', 'age'];
    protected $casts = [
        'email_verified_at' => 'datetime',
        'age'               => 'integer',
    ];
}
`
	ents := extract(t, "php_eloquent_orm_data", fi("app/Models/User.php", "php", src))

	// model class entity
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User model entity (SCOPE.Schema/model)")
	}
	// explicit $table property
	if !containsEntity(ents, "SCOPE.Schema", "app_users") {
		t.Error("expected app_users table entity from $table property")
	}
	// fillable columns
	for _, col := range []string{"name", "email", "age"} {
		if !containsEntity(ents, "SCOPE.Schema", col) {
			t.Errorf("expected fillable column %q", col)
		}
	}
	// casts columns
	if !containsEntity(ents, "SCOPE.Schema", "email_verified_at") {
		t.Error("expected email_verified_at column from $casts")
	}
}

// TestEloquentDeep_RelationshipExtractionWithRelatedModel verifies all relationship
// types are captured, with the related model name stored in properties.
func TestEloquentDeep_RelationshipExtractionWithRelatedModel(t *testing.T) {
	src := `<?php
class Post extends Model
{
    public function author(): BelongsTo
    {
        return $this->belongsTo(User::class);
    }
    public function comments(): HasMany
    {
        return $this->hasMany(Comment::class);
    }
    public function thumbnail(): HasOne
    {
        return $this->hasOne(Image::class);
    }
    public function tags(): BelongsToMany
    {
        return $this->belongsToMany(Tag::class);
    }
    public function history(): HasManyThrough
    {
        return $this->hasManyThrough(Revision::class, Edit::class);
    }
}
`
	ents := extract(t, "php_eloquent_orm_data", fi("Post.php", "php", src))

	// Each relationship method must produce a SCOPE.Component/relation entity.
	for _, rel := range []string{"author", "comments", "thumbnail", "tags", "history"} {
		if !containsEntity(ents, "SCOPE.Component", rel) {
			t.Errorf("expected relation entity for method %q", rel)
		}
	}
}

// TestEloquentDeep_MorphRelationships verifies morphTo and morphMany extraction.
func TestEloquentDeep_MorphRelationships(t *testing.T) {
	src := `<?php
class Image extends Model
{
    public function imageable()
    {
        return $this->morphTo();
    }
}

class Post extends Model
{
    public function images(): MorphMany
    {
        return $this->morphMany(Image::class, 'imageable');
    }
}
`
	ents := extract(t, "php_eloquent_orm_data", fi("Polymorphic.php", "php", src))

	if !containsEntity(ents, "SCOPE.Component", "imageable") {
		t.Error("expected imageable morphTo relation entity")
	}
	if !containsEntity(ents, "SCOPE.Component", "images") {
		t.Error("expected images morphMany relation entity")
	}
}

// TestEloquentDeep_ForeignKeyConvention verifies that a belongsTo method without
// an explicit FK arg produces the conventional snake_case FK column.
func TestEloquentDeep_ForeignKeyConvention(t *testing.T) {
	src := `<?php
class Comment extends Model
{
    public function post(): BelongsTo
    {
        return $this->belongsTo(Post::class);
    }
    public function postAuthor(): BelongsTo
    {
        return $this->belongsTo(User::class);
    }
}
`
	ents := extract(t, "php_eloquent_orm_data", fi("Comment.php", "php", src))

	// Conventional FKs: post → post_id; postAuthor → post_author_id
	if !containsEntity(ents, "SCOPE.Schema", "fk:post_id") {
		t.Error("expected fk:post_id from belongsTo(Post::class) convention")
	}
	if !containsEntity(ents, "SCOPE.Schema", "fk:post_author_id") {
		t.Error("expected fk:post_author_id from belongsTo(User::class) camelCase convention")
	}
}

// TestEloquentDeep_ForeignKeyExplicit verifies that an explicit 2nd arg FK overrides
// the conventional name.
func TestEloquentDeep_ForeignKeyExplicit(t *testing.T) {
	src := `<?php
class Profile extends Model
{
    public function owner(): BelongsTo
    {
        return $this->belongsTo(User::class, 'owner_id');
    }
}
`
	ents := extract(t, "php_eloquent_orm_data", fi("Profile.php", "php", src))

	if !containsEntity(ents, "SCOPE.Schema", "fk:owner_id") {
		t.Error("expected fk:owner_id from explicit 2nd arg in belongsTo")
	}
}

// TestEloquentDeep_MigrationForeignIdConstrained verifies the ->foreignId()->constrained() pattern.
func TestEloquentDeep_MigrationForeignIdConstrained(t *testing.T) {
	src := `<?php
use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    public function up(): void
    {
        Schema::create('posts', function (Blueprint $table) {
            $table->id();
            $table->foreignId('user_id')->constrained()->cascadeOnDelete();
            $table->foreignId('category_id')->constrained('categories');
            $table->string('title');
            $table->timestamps();
        });
    }

    public function down(): void
    {
        Schema::dropIfExists('posts');
    }
};
`
	ents := extract(t, "php_eloquent_orm_data", fi("database/migrations/2024_create_posts.php", "php", src))

	// Migration operation entities
	if !containsEntity(ents, "SCOPE.Operation", "create:posts") {
		t.Error("expected create:posts migration entity")
	}
	if !containsEntity(ents, "SCOPE.Operation", "drop:posts") {
		t.Error("expected drop:posts migration entity from dropIfExists")
	}
	// up/down methods
	if !containsEntity(ents, "SCOPE.Operation", "migration:up") {
		t.Error("expected migration:up from public function up()")
	}
	if !containsEntity(ents, "SCOPE.Operation", "migration:down") {
		t.Error("expected migration:down from public function down()")
	}
	// foreignId()->constrained()
	if !containsEntity(ents, "SCOPE.Schema", "fk:constrained:user_id") {
		t.Error("expected fk:constrained:user_id from foreignId()->constrained()")
	}
	// Blueprint string column
	if !containsEntity(ents, "SCOPE.Schema", "title") {
		t.Error("expected title column from Blueprint->string('title')")
	}
}

// TestEloquentDeep_LazyByDefault verifies that a model without $with produces the
// lazy:default marker (Eloquent is lazy by default).
func TestEloquentDeep_LazyByDefault(t *testing.T) {
	src := `<?php
class Article extends Model
{
    protected $fillable = ['title'];
    public function author(): BelongsTo
    {
        return $this->belongsTo(User::class);
    }
}
`
	ents := extract(t, "php_eloquent_orm_data", fi("Article.php", "php", src))

	if !containsEntity(ents, "SCOPE.Pattern", "lazy:default") {
		t.Error("expected lazy:default pattern — Eloquent is lazy by default when no $with is set")
	}
}

// TestEloquentDeep_EagerLoadWithCall verifies ->with() call-site eager loading detection.
func TestEloquentDeep_EagerLoadWithCall(t *testing.T) {
	src := `<?php
class PostController extends Controller
{
    public function index()
    {
        return Post::with(['author', 'comments'])->paginate();
    }
    public function show($id)
    {
        $post = Post::findOrFail($id);
        $post->load('author');
        return $post;
    }
}
`
	_ = src // controller source unused; use model source instead
	src2 := `<?php
class Post extends Model
{
    public function getWithAuthor()
    {
        return $this->with(['author'])->get();
    }
    public function getLoaded()
    {
        $post = Post::first();
        $post->load('tags');
        return $post;
    }
}
`
	ents := extract(t, "php_eloquent_orm_data", fi("Post.php", "php", src2))

	if !containsEntity(ents, "SCOPE.Pattern", "eager_load:with") {
		t.Error("expected eager_load:with entity from ->with() call")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "eager_load:load") {
		t.Error("expected eager_load:load entity from ->load() call")
	}
}

// TestEloquentDeep_MigrationAlterTable verifies Schema::table (alter) extraction.
func TestEloquentDeep_MigrationAlterTable(t *testing.T) {
	src := `<?php
use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

class AddStatusToOrdersTable extends Migration
{
    public function up()
    {
        Schema::table('orders', function (Blueprint $table) {
            $table->string('status')->default('pending');
            $table->unsignedBigInteger('assigned_user_id')->nullable();
            $table->foreign('assigned_user_id')->references('id')->on('users');
        });
    }

    public function down()
    {
        Schema::table('orders', function (Blueprint $table) {
            $table->dropColumn('status');
        });
    }
}
`
	ents := extract(t, "php_eloquent_orm_data", fi("2024_add_status_to_orders.php", "php", src))

	if !containsEntity(ents, "SCOPE.Operation", "alter:orders") {
		t.Error("expected alter:orders from Schema::table")
	}
	if !containsEntity(ents, "SCOPE.Operation", "migration:up") {
		t.Error("expected migration:up step")
	}
	if !containsEntity(ents, "SCOPE.Operation", "migration:down") {
		t.Error("expected migration:down step")
	}
	if !containsEntity(ents, "SCOPE.Schema", "status") {
		t.Error("expected status column from Blueprint->string")
	}
	if !containsEntity(ents, "SCOPE.Schema", "fk:assigned_user_id") {
		t.Error("expected fk:assigned_user_id from ->foreign()")
	}
}

// ============================================================================
// CycleORM
// ============================================================================

func TestCycleORMEntity(t *testing.T) {
	src := `<?php
namespace App\Entity;

use Cycle\Annotated\Annotation\Entity;
use Cycle\Annotated\Annotation\Column;

#[Entity(table: 'users')]
class User
{
    #[Column(type: 'primary')]
    private int $id;

    #[Column(type: 'string')]
    private string $name;
}
`
	ents := extract(t, "php_cycleorm_data", fi("User.php", "php", src))
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User entity model")
	}
	if !containsEntity(ents, "SCOPE.Schema", "id") {
		t.Error("expected id column entity")
	}
	if !containsEntity(ents, "SCOPE.Schema", "name") {
		t.Error("expected name column entity")
	}
}

func TestCycleORMRelation(t *testing.T) {
	src := `<?php
use Cycle\Annotated\Annotation\Relation\HasMany;
use Cycle\Annotated\Annotation\Relation\BelongsTo;

#[Entity]
class Post
{
    #[HasMany(target: Comment::class)]
    private array $comments;

    #[BelongsTo(target: User::class)]
    private User $author;
}
`
	ents := extract(t, "php_cycleorm_data", fi("Post.php", "php", src))
	if !containsEntity(ents, "SCOPE.Component", "relation:HasMany") {
		t.Error("expected HasMany relation")
	}
	if !containsEntity(ents, "SCOPE.Component", "relation:BelongsTo") {
		t.Error("expected BelongsTo relation")
	}
}

func TestCycleORMQuery(t *testing.T) {
	src := `<?php
class UserRepository
{
    public function findByEmail(string $email): ?User
    {
        return $this->orm->findOne(User::class, ['email' => $email]);
    }
}
`
	ents := extract(t, "php_cycleorm_data", fi("UserRepository.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "query:findOne") {
		t.Error("expected query:findOne entity")
	}
}

// ============================================================================
// Propel ORM
// ============================================================================

func TestPropelTableMap(t *testing.T) {
	src := `<?php
use Propel\Runtime\Map\TableMap;

class UserTableMap extends TableMap
{
    const COL_ID = 'user.id';
    const COL_USERNAME = 'user.username';
    const COL_EMAIL = 'user.email';
}
`
	ents := extract(t, "php_propel_orm_data", fi("UserTableMap.php", "php", src))
	if !containsEntity(ents, "SCOPE.Schema", "UserTableMap") {
		t.Error("expected UserTableMap schema entity")
	}
	if !containsEntity(ents, "SCOPE.Schema", "COL_USERNAME") {
		t.Error("expected COL_USERNAME column entity")
	}
}

func TestPropelRelation(t *testing.T) {
	src := `<?php
class BookTableMap extends TableMap
{
    public function initialize()
    {
        $this->addRelation('Author', AuthorTableMap::CLASS_DEFAULT, RelationMap::MANY_TO_ONE);
        $this->addForeignKey('author_id', 'id', 'INTEGER', 'author', 'id');
    }
}
`
	ents := extract(t, "php_propel_orm_data", fi("BookTableMap.php", "php", src))
	if !containsEntity(ents, "SCOPE.Component", "relation:Author") {
		t.Error("expected relation:Author component")
	}
	if !containsEntity(ents, "SCOPE.Schema", "foreign_key") {
		t.Error("expected foreign_key entity")
	}
}

func TestPropelQuery(t *testing.T) {
	src := `<?php
$users = UserQuery::create()
    ->filterByIsActive(true)
    ->find();

$book = BookQuery::create()->findOne();
`
	ents := extract(t, "php_propel_orm_data", fi("list.php", "php", src))
	if len(ents) == 0 {
		t.Error("expected Propel query entities from UserQuery::create()")
	}
}

// ============================================================================
// RedBeanPHP
// ============================================================================

func TestRedBeanDispense(t *testing.T) {
	src := `<?php
R::setup('mysql:host=localhost;dbname=shop', 'root', 'password');
$product = R::dispense('product');
$product->name = 'Widget';
$product->price = 9.99;
R::store($product);
`
	ents := extract(t, "php_redbeanphp_data", fi("store.php", "php", src))
	if !containsEntity(ents, "SCOPE.Schema", "product") {
		t.Error("expected product table schema from R::dispense")
	}
}

func TestRedBeanRelation(t *testing.T) {
	src := `<?php
R::associate($product, $category);
$related = R::related($product, 'category');
`
	ents := extract(t, "php_redbeanphp_data", fi("relate.php", "php", src))
	if !containsEntity(ents, "SCOPE.Component", "relation:associate") {
		t.Error("expected relation:associate component")
	}
	if !containsEntity(ents, "SCOPE.Component", "relation:related") {
		t.Error("expected relation:related component")
	}
}

func TestRedBeanFind(t *testing.T) {
	src := `<?php
$products = R::find('product', ' price > ? ', [10]);
$user = R::findOne('user', ' email = ? ', [$email]);
`
	ents := extract(t, "php_redbeanphp_data", fi("query.php", "php", src))
	if !containsEntity(ents, "SCOPE.Schema", "product") {
		t.Error("expected product schema from R::find")
	}
	if !containsEntity(ents, "SCOPE.Schema", "user") {
		t.Error("expected user schema from R::findOne")
	}
}

func TestRedBeanNoMatch(t *testing.T) {
	src := `<?php class Plain { public function run() {} }`
	ents := extract(t, "php_redbeanphp_data", fi("Plain.php", "php", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ============================================================================
// PHP SQL Driver Schema (mysql/postgres/sqlite)
// ============================================================================

func TestPHPSQLDriverMySQLCreateTable(t *testing.T) {
	src := `<?php
$pdo = new PDO("mysql:host=localhost;dbname=app", "root", "");
$pdo->exec("
    CREATE TABLE IF NOT EXISTS users (
        id INT AUTO_INCREMENT PRIMARY KEY,
        username VARCHAR(255) NOT NULL,
        email VARCHAR(255) UNIQUE
    )
");
`
	ents := extract(t, "php_sql_driver_schema", fi("setup.php", "php", src))
	if !containsEntity(ents, "SCOPE.Schema", "users") {
		t.Error("expected users table schema")
	}
	if !containsEntity(ents, "SCOPE.Schema", "users.username") {
		t.Error("expected users.username column schema")
	}
}

func TestPHPSQLDriverSQLiteCreateTable(t *testing.T) {
	src := `<?php
$db = new SQLite3('/tmp/test.db');
$db->exec('CREATE TABLE orders (
    id INTEGER PRIMARY KEY,
    total REAL NOT NULL,
    status TEXT DEFAULT "pending"
)');
`
	ents := extract(t, "php_sql_driver_schema", fi("init.php", "php", src))
	if !containsEntity(ents, "SCOPE.Schema", "orders") {
		t.Error("expected orders table schema")
	}
}

func TestPHPSQLDriverNoMatch(t *testing.T) {
	src := `<?php echo "no driver here";`
	ents := extract(t, "php_sql_driver_schema", fi("plain.php", "php", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ============================================================================
// Behat
// ============================================================================

func TestBehatFeature(t *testing.T) {
	src := `Feature: User login
  In order to access the application
  As a user
  I need to be able to log in

  Scenario: Successful login
    Given I am on the login page
    When I fill in "email" with "user@example.com"
    Then I should see "Dashboard"

  Scenario: Failed login
    Given I am on the login page
    When I fill in "email" with "bad@example.com"
    Then I should see "Invalid credentials"
`
	ents := extract(t, "php_behat_test", fi("login.feature", "gherkin", src))
	if !containsEntity(ents, "SCOPE.Operation", "feature:User login") {
		t.Error("expected feature entity")
	}
	if !containsEntity(ents, "SCOPE.Operation", "scenario:Successful login") {
		t.Error("expected Successful login scenario")
	}
	if !containsEntity(ents, "SCOPE.Operation", "scenario:Failed login") {
		t.Error("expected Failed login scenario")
	}
}

func TestBehatContextClass(t *testing.T) {
	src := `<?php
use Behat\Behat\Context\Context;

class FeatureContext implements Context
{
    /**
     * @Given I am on the login page
     */
    #[Given('/^I am on the login page$/')]
    public function iAmOnTheLoginPage()
    {
        // ...
    }

    /**
     * @When I fill in :field with :value
     */
    public function iFillIn($field, $value) {}
}
`
	ents := extract(t, "php_behat_test", fi("FeatureContext.php", "php", src))
	if !containsEntity(ents, "SCOPE.Component", "FeatureContext") {
		t.Error("expected FeatureContext context class entity")
	}
}

func TestBehatNoMatch(t *testing.T) {
	src := `<?php class NoBehatHere {}`
	ents := extract(t, "php_behat_test", fi("plain.php", "php", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ============================================================================
// Codeception
// ============================================================================

func TestCodeceptionCest(t *testing.T) {
	src := `<?php
use Codeception\Module\WebDriver;

class UserLoginCest
{
    public function loginSuccessfully(AcceptanceTester $I)
    {
        $I->amOnPage('/login');
        $I->fillField('email', 'user@example.com');
        $I->seeCurrentUrlEquals('/dashboard');
    }

    public function loginWithBadCredentials(AcceptanceTester $I)
    {
        $I->amOnPage('/login');
        $I->seeResponseContains('Invalid');
    }
}
`
	ents := extract(t, "php_codeception_test", fi("UserLoginCest.php", "php", src))
	if !containsEntity(ents, "SCOPE.Component", "UserLoginCest") {
		t.Error("expected UserLoginCest test suite")
	}
	if !containsEntity(ents, "SCOPE.Operation", "loginSuccessfully") {
		t.Error("expected loginSuccessfully test method")
	}
	if !containsEntity(ents, "SCOPE.Component", "AcceptanceTester") {
		t.Error("expected AcceptanceTester actor")
	}
}

func TestCodeceptionModule(t *testing.T) {
	src := `<?php
use Codeception\Module\Laravel;
use Codeception\Module\WebDriver;

class ApiCest
{
    public function checkEndpoint(FunctionalTester $I)
    {
        $I->sendGET('/api/users');
        $I->seeResponseCodeIs(200);
    }
}
`
	ents := extract(t, "php_codeception_test", fi("ApiCest.php", "php", src))
	if !containsEntity(ents, "SCOPE.Component", "Codeception\\Module\\Laravel") {
		t.Error("expected Laravel module dependency")
	}
}

func TestCodeceptionNoMatch(t *testing.T) {
	src := `<?php class NoCept {}`
	ents := extract(t, "php_codeception_test", fi("plain.php", "php", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ============================================================================
// Pest
// ============================================================================

func TestPestTestDeclarations(t *testing.T) {
	src := `<?php
uses(Tests\TestCase::class);

it('can create a user', function () {
    $user = User::factory()->create();
    expect($user->id)->toBeInt();
});

test('user email is unique', function () {
    // ...
});

describe('Authentication', function () {
    it('redirects guests to login', function () {
        // ...
    });
});
`
	ents := extract(t, "php_pest_test", fi("UserTest.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "can create a user") {
		t.Error("expected 'can create a user' test case")
	}
	if !containsEntity(ents, "SCOPE.Operation", "user email is unique") {
		t.Error("expected 'user email is unique' test case")
	}
	if !containsEntity(ents, "SCOPE.Component", "describe:Authentication") {
		t.Error("expected Authentication describe block")
	}
	if !containsEntity(ents, "SCOPE.Component", "Tests\\TestCase::class") {
		t.Error("expected uses(Tests\\TestCase::class) dependency")
	}
}

func TestPestDataset(t *testing.T) {
	src := `<?php
dataset('emails', [
    'user@example.com',
    'admin@example.com',
]);

it('validates emails', function (string $email) {
    expect($email)->toBeEmail();
})->with('emails');
`
	ents := extract(t, "php_pest_test", fi("EmailTest.php", "php", src))
	if !containsEntity(ents, "SCOPE.Schema", "dataset:emails") {
		t.Error("expected dataset:emails entity")
	}
}

func TestPestHooks(t *testing.T) {
	src := `<?php
uses(RefreshDatabase::class);

beforeEach(function () {
    $this->user = User::factory()->create();
});

afterEach(function () {
    // cleanup
});
`
	ents := extract(t, "php_pest_test", fi("HookTest.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "hook:beforeEach") {
		t.Error("expected hook:beforeEach pattern")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "hook:afterEach") {
		t.Error("expected hook:afterEach pattern")
	}
}

func TestPestNoMatch(t *testing.T) {
	src := `<?php echo "no pest here";`
	ents := extract(t, "php_pest_test", fi("plain.php", "php", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
