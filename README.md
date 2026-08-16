\# ⚡ Jolt



\*\*Java, but instant.\*\* Run a single `.java` file like a script — no Maven, no Gradle, no project setup.



Jolt compiles and runs standalone Java files on the fly, auto-resolving dependencies declared right inside the file.



\## Install



```bash

go install github.com/Bhumika-SN/jolt@latest

```



\## Usage



Run any Java file directly:



```bash

jolt Hello.java

```



\### With dependencies



Declare Maven dependencies and a required Java version right in the file using special comments:



```java

//DEPS com.google.code.gson:gson:2.10.1

//JAVA 17



import com.google.gson.Gson;



public class Demo {

&#x20;   public static void main(String\[] args) {

&#x20;       Gson gson = new Gson();

&#x20;       System.out.println(gson.toJson(new int\[]{1, 2, 3}));

&#x20;   }

}

```



```bash

jolt Demo.java

```



Jolt resolves `//DEPS` from Maven Central, caches the jars locally in `\~/.jolt/cache`, checks the `//JAVA` version against your installed JDK, then compiles and runs — all in one command.



\## Commands



| Command | Description |

|---|---|

| `jolt <file.java>` | Compile and run a Java file |

| `jolt init <file.java>` | Scaffold a new Java file |

| `jolt cache clear` | Clear the global dependency cache |

| `jolt version` | Show the installed Jolt version |



\## Examples



See the \[`examples/`](./examples) folder for sample `.java` files.



\## Why






## Why

Running a quick Java experiment usually means spinning up a full Maven/Gradle project just to test one file. Jolt skips that — point it at a `.java` file and it handles dependency resolution, version checking, compiling, and running for you.

### Transitive dependency resolution

Jolt doesn't just download the jars you list in `//DEPS` — it recursively resolves each dependency's own POM file from Maven Central, pulling in transitive runtime dependencies automatically (skipping `test`/`provided`/`optional` scopes). For example, declaring only OkHttp will automatically resolve and cache its Kotlin stdlib and Okio dependencies, without needing to list them manually.

