"""Sample FastAPI application — golden fixture source."""
from fastapi import FastAPI, Depends, HTTPException
from pydantic import BaseModel


app = FastAPI(title="Sample API", version="1.0.0")


class UserCreate(BaseModel):
    name: str
    email: str


class UserResponse(BaseModel):
    id: int
    name: str
    email: str


def get_db():
    """Dependency: yield a database session."""
    db = {"connected": True}
    yield db
    db["connected"] = False


@app.get("/health")
async def health_check():
    """Return service health."""
    return {"status": "ok"}


@app.get("/users/{user_id}", response_model=UserResponse)
async def get_user(user_id: int, db=Depends(get_db)):
    """Retrieve a user by ID."""
    if user_id <= 0:
        raise HTTPException(status_code=400, detail="Invalid user ID")
    return {"id": user_id, "name": "Alice", "email": "alice@example.com"}


@app.post("/users", response_model=UserResponse, status_code=201)
async def create_user(user: UserCreate, db=Depends(get_db)):
    """Create a new user."""
    return {"id": 1, "name": user.name, "email": user.email}


@app.delete("/users/{user_id}", status_code=204)
async def delete_user(user_id: int, db=Depends(get_db)):
    """Delete a user by ID."""
    if user_id <= 0:
        raise HTTPException(status_code=400, detail="Invalid user ID")
