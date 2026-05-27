using Microsoft.AspNetCore.Mvc;

namespace TrioFixture.Aspnet.Controllers;

[ApiController]
[Route("/api/[controller]")]
public class WidgetsController : ControllerBase
{
    [HttpGet]
    public IActionResult List()
    {
        return Ok(new[] { 1, 2, 3 });
    }

    [HttpGet("{id}")]
    public IActionResult Get(int id)
    {
        return Ok(new { id });
    }

    [HttpPost]
    public IActionResult Create([FromBody] object body)
    {
        return Created("/api/widgets", body);
    }
}
