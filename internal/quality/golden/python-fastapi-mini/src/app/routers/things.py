from fastapi import APIRouter, Depends

from app.deps import get_db

router = APIRouter(prefix="/api")


@router.get("/things")
def list_things(db = Depends(get_db)):
    """Return every thing."""
    return {"things": [], "db": db}
