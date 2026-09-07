namespace BlazorMini.Shared;

public interface IAppDisposable
{
    void Dispose();
}

public interface IAppTrackable
{
    void Track();
}

public class JsHelper : IJsHelper
{
    public void Invoke(string name) { }
}

public interface IJsHelper
{
    void Invoke(string name);
}
