using System;
using Microsoft.AspNetCore.Mvc;

namespace Example.Api
{
    [ApiController]
    [Route("api/users")]
    public class UserController : ControllerBase
    {
        private readonly IUserRepository _repo;

        public UserController(IUserRepository repo)
        {
            _repo = repo;
        }

        // CreateUser — env-gated, validated, try/catch controller action.
        [HttpPost]
        [ProducesResponseType(201)]
        public IActionResult CreateUser([FromBody] UserDto dto)
        {
            // env-gate: the feature is off unless SIGNUP_ENABLED is set.
            if (Environment.GetEnvironmentVariable("SIGNUP_ENABLED") != "true")
            {
                return StatusCode(503);
            }

            // 400 guard — email is required.
            if (dto.Email == null)
            {
                return BadRequest(new { error = "email required" });
            }

            try
            {
                // 409 guard inside the try — email already taken.
                if (_repo.ExistsByEmail(dto.Email))
                {
                    return Conflict(new { error = "email exists" });
                }

                var user = _repo.Save(dto);
                return CreatedAtAction(nameof(GetUser), new { id = user.Id }, user);
            }
            catch (Exception e)
            {
                _logger.LogError(e, "create failed");
                return StatusCode(500, new { error = "internal" });
            }
        }

        [HttpGet("{id}")]
        public IActionResult GetUser(int id) => Ok();
    }
}
