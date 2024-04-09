# The Nora Programming Language

Nora (named after Nora Sparkle) is a simple interpreted scripting language,
designed to be embedded in my personal tools.

It has a C-like syntax, supports variable bindings, prefix and infix operators,
has first-class and higher-order functions, can handle closures and has
integers, booleans, arrays and hashes built-in:

```
let greet = fn(name) {
  printf("Hello, %s", name);
}

greet("Ilya");
```

It's built with help of [Writing An Interpreter In Go](book.pdf) book.

## To-do

- [ ] Parsing:
  - [X] Prefix Operators
  - [ ] Infix Operators
  - [ ] Boolean Literals
  - [ ] Grouped Expressions
  - [ ] If Expressions
  - [ ] Function Literals
  - [ ] Call Expressions
- [ ] Evaluation:
  - [ ] Object System
  - [ ] Evaluating Expressions
  - [ ] Conditionals
  - [ ] Return Statements
  - [ ] Error Handling
  - [ ] Bindings & The Environment
  - [ ] Functions & Functions Calls
- [ ] Built-in Functions
- [ ] Strings
- [ ] Arrays
- [ ] Hashes
- [ ] Formatting
- [ ] Playground
