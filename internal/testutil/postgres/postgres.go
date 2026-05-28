package postgres

import (
	"context"
	"cv-solution/internal/migrator"
	"database/sql"
	"fmt"
	"net/url"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	defaultImage    = "pgvector/pgvector:pg16"
	defaultDatabase = "testdb"
	defaultUser     = "postgres"
	defaultPassword = "postgres"
)

type Option func(*Config)

type Config struct {
	Image      string
	Database   string
	Username   string
	Password   string
	Extensions []string
}

type Container struct {
	container testcontainers.Container
	config    Config
	dsn       string
}

func defaultConfig() Config {
	return Config{
		Image:      defaultImage,
		Database:   defaultDatabase,
		Username:   defaultUser,
		Password:   defaultPassword,
		Extensions: []string{"vector"},
	}
}

func Run(ctx context.Context, opts ...Option) (*Container, error) {
	cfg := defaultConfig()
	if opts != nil {
		for _, opt := range opts {
			opt(&cfg)
		}
	}

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        cfg.Image,
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       cfg.Database,
				"POSTGRES_USER":     cfg.Username,
				"POSTGRES_PASSWORD": cfg.Password,
			},
			WaitingFor: wait.ForAll(
				wait.ForListeningPort("5432/tcp"),
				wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
			).WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		return nil, err
	}

	helper := &Container{
		container: ctr,
		config:    cfg,
	}

	if err := helper.initialize(ctx); err != nil {
		_ = helper.Terminate(ctx)
		return nil, err
	}

	return helper, nil
}

func MustRun(t testing.TB, opts ...Option) *Container {
	t.Helper()

	helper, err := Run(t.Context(), opts...)
	if err != nil {
		t.Fatalf("start postgres testcontainer: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := helper.Terminate(cleanupCtx); err != nil {
			t.Fatalf("terminate postgres testcontainer: %v", err)
		}
	})

	return helper
}

func (c *Container) Config() Config {
	return c.config
}

func (c *Container) ConnectionString() string {
	return c.dsn
}

func (c *Container) OpenSQL(ctx context.Context) (*sql.DB, error) {
	db, err := sql.Open("pgx", c.dsn)
	if err != nil {
		return nil, err
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func (c *Container) OpenGorm(config *gorm.Config) (*gorm.DB, error) {
	if config == nil {
		config = &gorm.Config{}
	}

	// we run the migration
	db, err := gorm.Open(gormpostgres.Open(c.dsn), config)
	if err != nil {
		return nil, err
	}

	if err := migrator.Migrate(db); err != nil {
		return nil, err
	}

	return db, nil
}

func (c *Container) Terminate(ctx context.Context) error {
	if c == nil || c.container == nil {
		return nil
	}
	return c.container.Terminate(ctx)
}

func (c *Container) initialize(ctx context.Context) error {
	host, err := c.container.Host(ctx)
	if err != nil {
		return err
	}

	port, err := c.container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return err
	}

	c.dsn = buildConnectionString(c.config, host, port.Port())

	db, err := c.OpenSQL(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	for _, extension := range c.config.Extensions {
		if extension == "" {
			continue
		}
		query := fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s", extension)
		if _, err := db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("create extension %q: %w", extension, err)
		}
	}

	return nil
}

func buildConnectionString(cfg Config, host, port string) string {
	user := url.QueryEscape(cfg.Username)
	password := url.QueryEscape(cfg.Password)
	database := url.PathEscape(cfg.Database)
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, password, host, port, database)
}
