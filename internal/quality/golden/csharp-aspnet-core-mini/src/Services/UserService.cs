using System.Threading.Tasks;

namespace AspNetCoreMini.Services
{
    public class UserService : IUserService
    {
        public async Task<User> GetByIdAsync(int id)
        {
            // stubbed: real impl would query a DbContext
            return null;
        }

        public async Task<User> CreateAsync(CreateUserRequest request)
        {
            return new User { Id = 1, Name = request.Name };
        }

        public async Task<bool> DeleteAsync(int id)
        {
            return true;
        }
    }
}
