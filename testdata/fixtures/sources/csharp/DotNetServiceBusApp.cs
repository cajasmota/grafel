using Microsoft.AspNetCore.Mvc;
using Azure.Messaging.ServiceBus;
using Microsoft.EntityFrameworkCore;
using System.Text.Json;

/**
 * .NET Core MVC + Azure Service Bus fixture.
 * Demonstrates: HTTP endpoints, Service Bus producer, Service Bus consumer, DB access via EF Core.
 */

var builder = WebApplication.CreateBuilder(args);
builder.Services.AddControllers();
builder.Services.AddDbContext<OrderDbContext>(opts =>
    opts.UseSqlServer(builder.Configuration.GetConnectionString("DefaultConnection")));
builder.Services.AddSingleton(new ServiceBusClient(
    builder.Configuration["ServiceBus:ConnectionString"]));
builder.Services.AddHostedService<OrderEventConsumer>();
var app = builder.Build();
app.MapControllers();
app.Run();

// ── Domain ────────────────────────────────────────────────────────────────────

public class Order
{
    public int Id { get; set; }
    public string Product { get; set; } = string.Empty;
    public int Quantity { get; set; }
    public string Status { get; set; } = "Pending";
}

public class OrderDbContext : DbContext
{
    public OrderDbContext(DbContextOptions<OrderDbContext> options) : base(options) { }
    public DbSet<Order> Orders => Set<Order>();
}

// ── Controller ────────────────────────────────────────────────────────────────

[ApiController]
[Route("api/[controller]")]
public class OrdersController : ControllerBase
{
    private readonly OrderDbContext _db;
    private readonly ServiceBusClient _sbClient;

    public OrdersController(OrderDbContext db, ServiceBusClient sbClient)
    {
        _db = db;
        _sbClient = sbClient;
    }

    [HttpGet]
    public async Task<IActionResult> List() =>
        Ok(await _db.Orders.ToListAsync());

    [HttpGet("{id}")]
    public async Task<IActionResult> Get(int id)
    {
        var order = await _db.Orders.FindAsync(id);
        return order is null ? NotFound() : Ok(order);
    }

    [HttpPost]
    public async Task<IActionResult> Create([FromBody] Order order)
    {
        _db.Orders.Add(order);
        await _db.SaveChangesAsync();

        // Publish to Azure Service Bus
        var sender = _sbClient.CreateSender("orders.created");
        var message = new ServiceBusMessage(JsonSerializer.Serialize(order))
        {
            MessageId = order.Id.ToString(),
            ContentType = "application/json"
        };
        await sender.SendMessageAsync(message);
        await sender.DisposeAsync();

        return CreatedAtAction(nameof(Get), new { id = order.Id }, order);
    }

    [HttpDelete("{id}")]
    public async Task<IActionResult> Delete(int id)
    {
        var order = await _db.Orders.FindAsync(id);
        if (order is null) return NotFound();
        _db.Orders.Remove(order);
        await _db.SaveChangesAsync();
        return NoContent();
    }
}

// ── Consumer ─────────────────────────────────────────────────────────────────

public class OrderEventConsumer : BackgroundService
{
    private readonly ServiceBusClient _sbClient;
    private readonly IServiceScopeFactory _scopeFactory;

    public OrderEventConsumer(ServiceBusClient sbClient, IServiceScopeFactory scopeFactory)
    {
        _sbClient = sbClient;
        _scopeFactory = scopeFactory;
    }

    protected override async Task ExecuteAsync(CancellationToken stoppingToken)
    {
        var processor = _sbClient.CreateProcessor("orders.created", new ServiceBusProcessorOptions());
        processor.ProcessMessageAsync += HandleMessage;
        processor.ProcessErrorAsync += HandleError;
        await processor.StartProcessingAsync(stoppingToken);
        await Task.Delay(Timeout.Infinite, stoppingToken);
        await processor.StopProcessingAsync();
    }

    private async Task HandleMessage(ProcessMessageEventArgs args)
    {
        var body = args.Message.Body.ToString();
        var order = JsonSerializer.Deserialize<Order>(body);
        using var scope = _scopeFactory.CreateScope();
        var db = scope.ServiceProvider.GetRequiredService<OrderDbContext>();
        if (order is not null)
        {
            order.Status = "Processing";
            db.Orders.Update(order);
            await db.SaveChangesAsync();
        }
        await args.CompleteMessageAsync(args.Message);
    }

    private Task HandleError(ProcessErrorEventArgs args)
    {
        Console.Error.WriteLine($"Service Bus error: {args.Exception.Message}");
        return Task.CompletedTask;
    }
}
