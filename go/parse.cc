// parse.cc -- Go frontend parser.

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include "go-system.h"

#include "lex.h"
#include "gogo.h"
#include "go-diagnostics.h"
#include "types.h"
#include "statements.h"
#include "expressions.h"
#include "export.h"
#include "import.h"
#include "parse.h"

// Generics (gccgo extension).
//
// This frontend implements Go generics by monomorphization driven by
// re-parsing.  When a generic function declaration is parsed, its type
// parameter names and the tokens making up its signature and body are
// captured into a Generic_function_info, but no function is compiled.
// At each instantiation site (F[int]) the captured tokens are copied
// with every occurrence of a type parameter name textually replaced by
// the tokens of the corresponding type argument, and the result is
// re-parsed as an ordinary, fully concrete function.  Each distinct set
// of type arguments produces one instance, cached by a mangled key.

// A captured method declaration on a generic type, e.g.
// "func (s *Stack[T]) Push(x T) {...}".  When the generic type is
// instantiated, each method template is re-parsed with the receiver's
// type parameter names substituted by the concrete type arguments,
// producing an ordinary method on the instance.

struct Generic_method_template
{
  // The type parameter names as written in the receiver, e.g. [T] or
  // [K, V].  These may differ from the type's own parameter names.
  std::vector<std::string> recv_type_param_names;
  // The tokens of the whole method declaration, from the receiver "("
  // through the body, EOF-terminated.
  std::vector<Token> tokens;
};

// A use of a generic type that appeared before the type's declaration
// (a forward reference).  A placeholder type declaration stands in for
// it during parsing; after parsing, the placeholder is made an alias of
// the real instantiation.

struct Pending_generic_type
{
  // The placeholder type declaration returned at the use site.
  Named_object* placeholder;
  // The (packed) name of the generic type being referenced.  Used when INFO
  // is NULL (a forward reference to a not-yet-declared generic type).
  std::string generic_name;
  // The generic template, when it is already known but a TYPE ARGUMENT is a
  // forward reference that cannot be canonicalized yet (e.g. a package-level
  // type alias declared in another file).  When non-NULL, resolution re-uses
  // this template directly and re-canonicalizes the arguments.  NULL for the
  // generic_name path above.
  Generic_function_info* info;
  // The type arguments, each a captured token sequence.
  std::vector<std::vector<Token> > type_args;
  // Package-name -> pkgpath bindings for the type arguments' qualifiers,
  // captured at the deferral site while they still resolve (file-scope imports
  // and replay aliases are gone by the post-parse resolution pass).  Without
  // this, a qualifier that is ambiguous by name (e.g. two loaded packages both
  // named "tpm2": go-tpm's tpm2 and legacy/tpm2) would resolve to an arbitrary
  // same-named package during resolution and the type argument would be
  // "undefined".
  std::map<std::string, std::string> pkg_bindings;
  Location location;
};

// A recorded obligation that a type argument satisfy its type
// parameter's constraint, checked after types are determined.

struct Constraint_obligation
{
  // The type argument, as a captured token sequence.
  std::vector<Token> arg;
  // The constraint, as a captured token sequence.
  std::vector<Token> constraint;
  // The displayed name of the generic being instantiated.
  std::string what;
  // The names of the generic's type parameters.  A constraint that
  // mentions one of them (e.g. "lesser[T]" in "[T lesser[T]]") depends on
  // the type parameters and is left to be checked when the instance body
  // is compiled, rather than resolved here.
  std::vector<std::string> tparams;
  // The package-qualifier alias->pkgpath map to resolve qualifiers in "arg"
  // and "constraint" when they are re-parsed later (deferred until after
  // types are determined).  Combines the template's own imports with the
  // caller's bindings for the type arguments, so a cross-package type
  // argument resolves to the exact package it came from even when a
  // same-named package is imported elsewhere.
  std::map<std::string, std::string> pkg_aliases;
  // The package that defined the generic being instantiated, if imported;
  // NULL for a locally-declared generic.  A constraint captured from an
  // imported template refers to that package's types by their bare names
  // (e.g. "Marshallable" or a named type-set "Contents"); resolving the
  // constraint during the deferred check must enter this package's context
  // so those bare names resolve to it rather than being reported undefined.
  Package* defining_package;
  Location location;
};

class Generic_function_info
{
 public:
  Generic_function_info(const std::string& name, bool is_exported,
			Location location)
    : name_(name), is_exported_(is_exported), location_(location),
      type_param_names_(), tokens_(), instances_(), marker_signature_(NULL),
      methods_(), defining_package_(NULL), package_aliases_(),
      is_function_local_(false), decl_bindings_(NULL), enclosing_type_args_()
  { }

  // For a function-local generic type declared inside a GENERIC function, the
  // resolved type arguments of that enclosing instantiation, as source-token
  // lists (e.g. F[string] -> {"string"}).  Used to render the instance's
  // reflection name as "Base[enclosingArgs;ownArgs]" (matching gc).  Empty for
  // a local type in a non-generic function or a package-level template.
  std::vector<std::vector<Token> >&
  enclosing_type_args()
  { return this->enclosing_type_args_; }

  void
  set_enclosing_type_args(const std::vector<std::vector<Token> >& a)
  { this->enclosing_type_args_ = a; }

  // For a function-local generic type, the block/function bindings contour in
  // which it was DECLARED.  Its instance body must be replayed resolving
  // generic-type names in this contour (its lexical declaration scope), not in
  // the use-site scope where the instance happens to be created -- otherwise a
  // same-named type in an inner block at the use site would wrongly shadow the
  // one visible where the template was declared.  NULL for package-level
  // templates.
  Bindings*
  decl_bindings() const
  { return this->decl_bindings_; }

  void
  set_decl_bindings(Bindings* b)
  { this->decl_bindings_ = b; }

  // Whether this generic type template was declared inside a function body
  // (a function-local generic type).  Such a template is never exported or
  // imported, and -- crucially when the enclosing function is itself generic
  // -- each enclosing instantiation captures its own token stream (with the
  // enclosing type parameters already substituted), so its instances must NOT
  // share the cross-package canonical-instance registry: F[string].Box[int]
  // and F[float64].Box[int] are distinct nominal types.  Instead each instance
  // is created in-function, so its backend/reflection name incorporates the
  // (distinct) enclosing function instance.
  bool
  is_function_local() const
  { return this->is_function_local_; }

  void
  set_is_function_local()
  { this->is_function_local_ = true; }

  // Map from a package qualifier alias used in the template tokens (e.g.
  // "internal") to that package's full pkgpath, captured when the template
  // is parsed (while the defining file's import scope is in effect).  Used
  // when re-parsing an instance, where the file scope is gone and a bare
  // alias would otherwise resolve to the wrong same-named package.
  std::map<std::string, std::string>&
  package_aliases()
  { return this->package_aliases_; }

  // The package that defined this template, if it was imported from
  // another package; NULL for a locally-declared template.  Used so that,
  // while re-parsing an imported template, bare references to the defining
  // package's other symbols (generic templates and ordinary exported
  // declarations) resolve.
  Package*
  defining_package() const
  { return this->defining_package_; }

  void
  set_defining_package(Package* p)
  { this->defining_package_ = p; }

  // The method templates declared on a generic type.
  std::vector<Generic_method_template>&
  methods()
  { return this->methods_; }

  // The constraint tokens for each type parameter (parallel to
  // type_param_names()).  An empty entry means no recorded constraint.
  std::vector<std::vector<Token> >&
  constraints()
  { return this->constraints_; }

  // The packed name of the generic function.
  const std::string&
  name() const
  { return this->name_; }

  bool
  is_exported() const
  { return this->is_exported_; }

  Location
  location() const
  { return this->location_; }

  // The raw (source) names of the type parameters, in order.
  std::vector<std::string>&
  type_param_names()
  { return this->type_param_names_; }

  // The captured tokens: the signature and body, EOF-terminated.
  std::vector<Token>&
  tokens()
  { return this->tokens_; }

  // Look up a previously created instance by its mangled key, or NULL.
  Named_object*
  find_instance(const std::string& key)
  {
    Unordered_map(std::string, Named_object*)::iterator p =
      this->instances_.find(key);
    if (p == this->instances_.end())
      return NULL;
    return p->second;
  }

  void
  add_instance(const std::string& key, Named_object* no,
	       const std::vector<std::vector<Token> >& type_args)
  {
    this->instances_[key] = no;
    this->instance_list_.push_back(std::make_pair(no, type_args));
  }

  // All instances created so far, paired with their type arguments.  Used to
  // retroactively instantiate a method on instances that were created before
  // the method was declared (a generic type used -- e.g. in an interface
  // satisfaction check "var _ I = &T[int]{}" -- above its method decls).
  std::vector<std::pair<Named_object*, std::vector<std::vector<Token> > > >&
  instance_list()
  { return this->instance_list_; }

  // The cached signature parsed with marker types substituted for the
  // type parameters, used for type-argument inference.  NULL until built.
  Function_type*
  marker_signature() const
  { return this->marker_signature_; }

  void
  set_marker_signature(Function_type* ft)
  { this->marker_signature_ = ft; }

 private:
  std::string name_;
  bool is_exported_;
  Location location_;
  std::vector<std::string> type_param_names_;
  std::vector<Token> tokens_;
  Unordered_map(std::string, Named_object*) instances_;
  Function_type* marker_signature_;
  std::vector<Generic_method_template> methods_;
  std::vector<std::vector<Token> > constraints_;
  Package* defining_package_;
  std::map<std::string, std::string> package_aliases_;
  std::vector<std::pair<Named_object*, std::vector<std::vector<Token> > > >
    instance_list_;
  bool is_function_local_;
  Bindings* decl_bindings_;
  std::vector<std::vector<Token> > enclosing_type_args_;
};

// Generics: cross-package export/import of generic templates.
//
// Instantiation works by re-parsing a captured token stream with the
// type-parameter names textually substituted.  To make a generic
// function or type usable from another package we therefore serialize
// that token stream (together with the type-parameter names and their
// constraints, and any method templates) into the export data, and
// reconstruct it on import.  The format is a small, self-describing
// textual encoding; numeric and string token values are length-prefixed
// so the data is binary-safe.

// Write a length-prefixed raw byte string as "<len> <bytes>".

static void
gen_write_lenstr(Export* exp, const std::string& s)
{
  exp->write_int(static_cast<int>(s.length()));
  exp->write_c_string(" ");
  exp->write_string(s);
}

// Write a single token.

static void
gen_write_token(Export* exp, const Token& t)
{
  switch (t.classification())
    {
    case Token::TOKEN_EOF:
      exp->write_c_string("e\n");
      break;
    case Token::TOKEN_KEYWORD:
      exp->write_c_string("k ");
      exp->write_int(static_cast<int>(t.keyword()));
      exp->write_c_string("\n");
      break;
    case Token::TOKEN_OPERATOR:
      exp->write_c_string("o ");
      exp->write_int(static_cast<int>(t.op()));
      exp->write_c_string("\n");
      break;
    case Token::TOKEN_IDENTIFIER:
      exp->write_c_string("i ");
      exp->write_int(t.is_identifier_exported() ? 1 : 0);
      exp->write_c_string(" ");
      gen_write_lenstr(exp, t.identifier());
      exp->write_c_string("\n");
      break;
    case Token::TOKEN_STRING:
      exp->write_c_string("s ");
      gen_write_lenstr(exp, t.string_value());
      exp->write_c_string("\n");
      break;
    case Token::TOKEN_INTEGER:
    case Token::TOKEN_CHARACTER:
      {
	const mpz_t* val = (t.classification() == Token::TOKEN_INTEGER
			    ? t.integer_value()
			    : t.character_value());
	char* s = mpz_get_str(NULL, 16, *val);
	exp->write_c_string(t.classification() == Token::TOKEN_INTEGER
			    ? "n " : "c ");
	gen_write_lenstr(exp, std::string(s));
	exp->write_c_string("\n");
	free(s);
      }
      break;
    case Token::TOKEN_FLOAT:
    case Token::TOKEN_IMAGINARY:
      {
	const mpfr_t* val = (t.classification() == Token::TOKEN_FLOAT
			     ? t.float_value()
			     : t.imaginary_value());
	mpfr_exp_t e;
	char* s = mpfr_get_str(NULL, &e, 10, 0, *val, MPFR_RNDN);
	std::string out;
	if (*s == '-')
	  out += '-';
	out += "0.";
	out += (*s == '-') ? s + 1 : s;
	char buf[32];
	snprintf(buf, sizeof buf, "E%ld", static_cast<long>(e));
	out += buf;
	mpfr_free_str(s);
	exp->write_c_string(t.classification() == Token::TOKEN_FLOAT
			    ? "f " : "m ");
	gen_write_lenstr(exp, out);
	exp->write_c_string("\n");
      }
      break;
    default:
      exp->write_c_string("v\n");
      break;
    }
}

// Write a token vector as "<count>\n" followed by the tokens.

static void
gen_write_tokens(Export* exp, const std::vector<Token>& toks)
{
  exp->write_int(static_cast<int>(toks.size()));
  exp->write_c_string("\n");
  for (size_t i = 0; i < toks.size(); ++i)
    gen_write_token(exp, toks[i]);
}

// Write the body of one generic template: its type-parameter names and
// constraints, its signature/body tokens, and any method templates.

// Defined below: rewrite a constraint into a form that an importing
// package can enforce on its own, expanding a named type-set constraint
// (e.g. "Ordered") to its inline basic type-set ("~int | ... | ~string").
static std::vector<Token>
expand_constraint_for_export(Gogo*, const std::vector<Token>&);

static void
gen_write_generic_body(Export* exp, Gogo* gogo, Generic_function_info* info)
{
  std::vector<std::string>& names = info->type_param_names();
  std::vector<std::vector<Token> >& constraints = info->constraints();
  exp->write_int(static_cast<int>(names.size()));
  exp->write_c_string("\n");
  for (size_t i = 0; i < names.size(); ++i)
    {
      gen_write_lenstr(exp, names[i]);
      exp->write_c_string("\n");
      if (i < constraints.size())
	{
	  std::vector<Token> c =
	    expand_constraint_for_export(gogo, constraints[i]);
	  gen_write_tokens(exp, c);
	}
      else
	{
	  exp->write_int(0);
	  exp->write_c_string("\n");
	}
    }
  gen_write_tokens(exp, info->tokens());

  std::vector<Generic_method_template>& methods = info->methods();
  exp->write_int(static_cast<int>(methods.size()));
  exp->write_c_string("\n");
  for (size_t i = 0; i < methods.size(); ++i)
    {
      std::vector<std::string>& rp = methods[i].recv_type_param_names;
      exp->write_int(static_cast<int>(rp.size()));
      exp->write_c_string("\n");
      for (size_t j = 0; j < rp.size(); ++j)
	{
	  gen_write_lenstr(exp, rp[j]);
	  exp->write_c_string("\n");
	}
      gen_write_tokens(exp, methods[i].tokens);
    }

  // The package-qualifier alias->pkgpath map, so an importer re-parsing this
  // template resolves a qualifier (e.g. "attr.Filter") to the exact package
  // the template was written against, rather than relying on an ambiguous
  // by-name lookup.
  std::map<std::string, std::string>& aliases = info->package_aliases();
  exp->write_int(static_cast<int>(aliases.size()));
  exp->write_c_string("\n");
  for (std::map<std::string, std::string>::const_iterator p = aliases.begin();
       p != aliases.end();
       ++p)
    {
      gen_write_lenstr(exp, p->first);
      exp->write_c_string(" ");
      gen_write_lenstr(exp, p->second);
      exp->write_c_string("\n");
    }
}

// Export all exported generic function and type templates of the
// current package.

void
go_export_generics(Export* exp, Gogo* gogo)
{
  std::vector<std::pair<std::string, Generic_function_info*> > funcs;
  const Unordered_map(std::string, Generic_function_info*)& gf =
    gogo->generic_functions();
  // Export every generic template, not just the exported ones: an exported
  // generic function or type's body may reference the package's unexported
  // generic helpers (e.g. an exported constructor returning an unexported
  // generic type), which an importer must be able to instantiate too.
  for (Unordered_map(std::string, Generic_function_info*)::const_iterator p =
	 gf.begin();
       p != gf.end();
       ++p)
    if (p->second->defining_package() == NULL)
      funcs.push_back(*p);

  std::vector<std::pair<std::string, Generic_function_info*> > types;
  const Unordered_map(std::string, Generic_function_info*)& gt =
    gogo->generic_types();
  for (Unordered_map(std::string, Generic_function_info*)::const_iterator p =
	 gt.begin();
       p != gt.end();
       ++p)
    // A function-local generic type is never visible outside its enclosing
    // function, so it must not appear in this package's export data.
    if (p->second->defining_package() == NULL
	&& !p->second->is_function_local())
      types.push_back(*p);

  if (funcs.empty() && types.empty())
    return;

  std::sort(funcs.begin(), funcs.end());
  std::sort(types.begin(), types.end());

  exp->write_c_string("generics ");
  exp->write_int(static_cast<int>(funcs.size()));
  exp->write_c_string(" ");
  exp->write_int(static_cast<int>(types.size()));
  exp->write_c_string("\n");

  // The import paths of packages referenced by the template bodies.  An
  // importer must fully import these so that, when it instantiates a
  // template, qualified references such as "strings.Join" resolve.
  const Unordered_set(const Package*)& gp = gogo->generic_imported_packages();
  std::vector<std::string> gpaths;
  for (Unordered_set(const Package*)::const_iterator p = gp.begin();
       p != gp.end();
       ++p)
    gpaths.push_back((*p)->pkgpath());
  std::sort(gpaths.begin(), gpaths.end());
  exp->write_c_string("genimports ");
  exp->write_int(static_cast<int>(gpaths.size()));
  exp->write_c_string("\n");
  for (size_t i = 0; i < gpaths.size(); ++i)
    {
      gen_write_lenstr(exp, gpaths[i]);
      exp->write_c_string("\n");
    }

  for (size_t i = 0; i < funcs.size(); ++i)
    {
      exp->write_c_string("gfunc ");
      gen_write_lenstr(exp, funcs[i].second->name());
      exp->write_c_string("\n");
      gen_write_generic_body(exp, gogo, funcs[i].second);
    }
  for (size_t i = 0; i < types.size(); ++i)
    {
      exp->write_c_string("gtype ");
      gen_write_lenstr(exp, types[i].second->name());
      exp->write_c_string("\n");
      gen_write_generic_body(exp, gogo, types[i].second);
    }
}

// Scan one token stream of an exported generic template and add to
// EXPORTS any package-scope symbol it references by a bare (unqualified)
// name.  TYPE_PARAMS are the template's own type-parameter names, which
// are not package symbols.  Identifiers in selector position (after ".")
// are skipped, as are universe names (which are not in package bindings).

static void
collect_refs_in_tokens(Gogo* gogo, const Bindings* bindings,
		       const std::vector<Token>& toks,
		       const std::vector<std::string>& type_params,
		       Unordered_set(Named_object*)* exports)
{
  bool prev_dot = false;
  for (size_t i = 0; i < toks.size(); ++i)
    {
      const Token& t = toks[i];
      bool this_dot = t.is_op(OPERATOR_DOT);
      if (!t.is_identifier())
	{
	  prev_dot = this_dot;
	  continue;
	}
      if (prev_dot)
	{
	  prev_dot = false;
	  continue;
	}
      prev_dot = false;

      const std::string& name = t.identifier();

      // Skip the template's own type parameters.
      bool is_type_param = false;
      for (size_t j = 0; j < type_params.size(); ++j)
	if (type_params[j] == name)
	  {
	    is_type_param = true;
	    break;
	  }
      if (is_type_param)
	continue;

      // Generic templates are exported through the generics section, not
      // as ordinary symbols; their placeholder declarations must not be
      // exported here.
      if (gogo->lookup_generic_function(name) != NULL)
	continue;

      // Look the name up in package scope.  Locals are not found here.
      Named_object* no =
	bindings->lookup(gogo->pack_hidden_name(name,
						Lex::is_exported_name(name)));
      if (no == NULL)
	continue;
      // Skip universe/predeclared names (int, append, ...) and anything
      // imported from another package; only this package's own
      // declarations need to be added to the export set.
      if (Linemap::is_predeclared_location(no->location())
	  || no->package() != NULL)
	continue;
      if (no->is_function()
	  || no->is_function_declaration()
	  || no->is_type()
	  || no->is_const()
	  || no->is_variable())
	{
	  // An unexported helper would otherwise get a local (static)
	  // symbol; mark it referenced-by-inline so the backend gives it
	  // external linkage, allowing instantiations in other packages
	  // to call it.
	  if (no->is_function())
	    no->func_value()->set_is_referenced_by_inline();
	  else if (no->is_variable())
	    no->var_value()->set_is_referenced_by_inline();
	  exports->insert(no);
	}
    }
}

void
go_collect_generic_exports(Gogo* gogo, const Bindings* bindings,
			   Unordered_set(Named_object*)* exports)
{
  const Unordered_map(std::string, Generic_function_info*)& gf =
    gogo->generic_functions();
  for (Unordered_map(std::string, Generic_function_info*)::const_iterator p =
	 gf.begin();
       p != gf.end();
       ++p)
    {
      // Every locally-declared generic template is exported (see
      // go_export_generics), including unexported ones, so collect the
      // symbols referenced by all of them -- an unexported generic helper's
      // body may reference further unexported types/funcs/vars that an
      // importer must be able to resolve and link against.  Skip only
      // templates imported from another package.
      if (p->second->defining_package() != NULL)
	continue;
      Generic_function_info* info = p->second;
      collect_refs_in_tokens(gogo, bindings, info->tokens(),
			     info->type_param_names(), exports);
    }

  const Unordered_map(std::string, Generic_function_info*)& gt =
    gogo->generic_types();
  for (Unordered_map(std::string, Generic_function_info*)::const_iterator p =
	 gt.begin();
       p != gt.end();
       ++p)
    {
      // Function-local generic types are not exported (see go_export_generics),
      // so do not collect symbols on their behalf.
      if (p->second->defining_package() != NULL
	  || p->second->is_function_local())
	continue;
      Generic_function_info* info = p->second;
      collect_refs_in_tokens(gogo, bindings, info->tokens(),
			     info->type_param_names(), exports);
      for (size_t m = 0; m < info->methods().size(); ++m)
	collect_refs_in_tokens(gogo, bindings, info->methods()[m].tokens,
			       info->methods()[m].recv_type_param_names,
			       exports);
    }
}

// Read a non-negative-or-negative integer, skipping leading spaces.

static int
gen_read_int(Import* imp)
{
  while (imp->peek_char() == ' ')
    imp->get_char();
  bool neg = false;
  if (imp->peek_char() == '-')
    {
      neg = true;
      imp->get_char();
    }
  int v = 0;
  while (true)
    {
      int c = imp->peek_char();
      if (c >= '0' && c <= '9')
	{
	  v = v * 10 + (c - '0');
	  imp->get_char();
	}
      else
	break;
    }
  return neg ? -v : v;
}

// Consume a single trailing newline if present.

static void
gen_skip_newline(Import* imp)
{
  if (imp->peek_char() == '\n')
    imp->get_char();
}

// Read a length-prefixed raw byte string written by gen_write_lenstr.

static std::string
gen_read_lenstr(Import* imp)
{
  int len = gen_read_int(imp);
  if (imp->peek_char() == ' ')
    imp->get_char();
  std::string s;
  if (len > 0)
    imp->read(static_cast<size_t>(len), &s);
  return s;
}

// Read a single token written by gen_write_token.

static Token
gen_read_token(Import* imp, Location loc)
{
  int code = imp->get_char();
  switch (code)
    {
    case 'k':
      {
	int kw = gen_read_int(imp);
	gen_skip_newline(imp);
	return Token::make_keyword_token(static_cast<Keyword>(kw), loc);
      }
    case 'o':
      {
	int op = gen_read_int(imp);
	gen_skip_newline(imp);
	return Token::make_operator_token(static_cast<Operator>(op), loc);
      }
    case 'i':
      {
	int isexp = gen_read_int(imp);
	std::string name = gen_read_lenstr(imp);
	gen_skip_newline(imp);
	return Token::make_identifier_token(name, isexp != 0, loc);
      }
    case 's':
      {
	std::string s = gen_read_lenstr(imp);
	gen_skip_newline(imp);
	return Token::make_string_token(s, loc);
      }
    case 'n':
    case 'c':
      {
	std::string s = gen_read_lenstr(imp);
	gen_skip_newline(imp);
	mpz_t v;
	mpz_init(v);
	mpz_set_str(v, s.c_str(), 16);
	Token t = (code == 'n'
		   ? Token::make_integer_token(v, loc)
		   : Token::make_character_token(v, loc));
	mpz_clear(v);
	return t;
      }
    case 'f':
    case 'm':
      {
	std::string s = gen_read_lenstr(imp);
	gen_skip_newline(imp);
	mpfr_t v;
	mpfr_init_set_str(v, s.c_str(), 10, MPFR_RNDN);
	Token t = (code == 'f'
		   ? Token::make_float_token(v, loc)
		   : Token::make_imaginary_token(v, loc));
	mpfr_clear(v);
	return t;
      }
    case 'e':
      gen_skip_newline(imp);
      return Token::make_eof_token(loc);
    default:
      gen_skip_newline(imp);
      return Token::make_invalid_token(loc);
    }
}

// Read a token vector written by gen_write_tokens.

static void
gen_read_tokens(Import* imp, std::vector<Token>* out, Location loc)
{
  int n = gen_read_int(imp);
  gen_skip_newline(imp);
  for (int i = 0; i < n; ++i)
    out->push_back(gen_read_token(imp, loc));
}

// Read the body of one generic template written by gen_write_generic_body.

static void
gen_read_generic_body(Import* imp, Generic_function_info* info, Location loc)
{
  int nparams = gen_read_int(imp);
  gen_skip_newline(imp);
  for (int i = 0; i < nparams; ++i)
    {
      std::string nm = gen_read_lenstr(imp);
      gen_skip_newline(imp);
      info->type_param_names().push_back(nm);
      std::vector<Token> c;
      gen_read_tokens(imp, &c, loc);
      info->constraints().push_back(c);
    }
  gen_read_tokens(imp, &info->tokens(), loc);

  int nmeth = gen_read_int(imp);
  gen_skip_newline(imp);
  for (int i = 0; i < nmeth; ++i)
    {
      Generic_method_template mt;
      int nrp = gen_read_int(imp);
      gen_skip_newline(imp);
      for (int j = 0; j < nrp; ++j)
	{
	  std::string nm = gen_read_lenstr(imp);
	  gen_skip_newline(imp);
	  mt.recv_type_param_names.push_back(nm);
	}
      gen_read_tokens(imp, &mt.tokens, loc);
      info->methods().push_back(mt);
    }

  // The package-qualifier alias->pkgpath map (see gen_write_generic_body).
  int nalias = gen_read_int(imp);
  gen_skip_newline(imp);
  for (int i = 0; i < nalias; ++i)
    {
      std::string alias = gen_read_lenstr(imp);
      std::string path = gen_read_lenstr(imp);
      gen_skip_newline(imp);
      info->package_aliases()[alias] = path;
    }
}

// Read the "generics" section of import data and register the templates
// in GOGO, associated with PACKAGE.

void
go_import_generics(Import* imp, Gogo* gogo, Package* package)
{
  Location loc = imp->location();
  imp->require_c_string("generics ");
  int nfunc = gen_read_int(imp);
  int ntype = gen_read_int(imp);
  gen_skip_newline(imp);

  // Fully import the packages referenced by the template bodies, so that
  // qualified references resolve when the templates are instantiated.
  imp->require_c_string("genimports ");
  int nimp = gen_read_int(imp);
  gen_skip_newline(imp);
  std::vector<std::string> gpaths;
  for (int i = 0; i < nimp; ++i)
    {
      gpaths.push_back(gen_read_lenstr(imp));
      gen_skip_newline(imp);
    }
  for (int i = 0; i < nimp; ++i)
    {
      gogo->import_package(gpaths[i], "_", false, false, loc);
      // Mark it as referenced-by-a-generic-template so the importing file's
      // unused-import check does not flag it (it is imported by the compiler
      // to instantiate the template, not by user source).
      Package* gp = gogo->package_from_pkgpath(gpaths[i]);
      if (gp != NULL)
	gogo->add_generic_imported_package(gp);
    }

  for (int i = 0; i < nfunc; ++i)
    {
      imp->require_c_string("gfunc ");
      std::string name = gen_read_lenstr(imp);
      gen_skip_newline(imp);
      Generic_function_info* info =
	new Generic_function_info(name, true, loc);
      info->set_defining_package(package);
      gen_read_generic_body(imp, info, loc);
      std::string key = package->pkgpath() + '.' + name;
      if (gogo->lookup_generic_function(key) == NULL)
	{
	  gogo->add_generic_function(key, info);
	  // A placeholder declaration so that "pkg.Name" resolves; it is
	  // never compiled, every use is rewritten to an instance.
	  Function_type* placeholder =
	    Type::make_function_type(NULL, NULL, NULL, loc);
	  package->add_function_declaration(name, placeholder, loc);
	}
    }
  for (int i = 0; i < ntype; ++i)
    {
      imp->require_c_string("gtype ");
      std::string name = gen_read_lenstr(imp);
      gen_skip_newline(imp);
      Generic_function_info* info =
	new Generic_function_info(name, true, loc);
      info->set_defining_package(package);
      gen_read_generic_body(imp, info, loc);
      std::string key = package->pkgpath() + '.' + name;
      if (gogo->lookup_generic_type(key) == NULL)
	gogo->add_generic_type(key, info);
    }
}

// Generics: read the "geninsts" section written by Export::write_generic_
// instances and stamp each imported generic instance type with its
// package-independent canonical id.  Register imported instances in the
// compilation-global canonical-instance table as well, so a later local use
// of the same imported generic instance reuses this Named_object instead of
// creating a second identical local instance with a duplicate unnamed
// underlying type.  If the local package later needs methods on the reused
// instance, instantiate_generic_type handles that at the reuse site.

void
go_import_generic_instances(Import* imp, Gogo* gogo)
{
  imp->require_c_string("geninsts ");
  int n = gen_read_int(imp);
  gen_skip_newline(imp);
  for (int i = 0; i < n; ++i)
    {
      std::string id = gen_read_lenstr(imp);
      if (imp->peek_char() == ' ')
	imp->get_char();
      Type* type = imp->read_type();
      gen_skip_newline(imp);
      if (type == NULL)
	continue;
      Named_type* nt = type->named_type();
      if (nt != NULL && nt->named_object() != NULL && !id.empty())
	{
	  nt->set_generic_canonical_id(id);
	  if (gogo->lookup_canonical_generic_instance(id) == NULL)
	    gogo->add_canonical_generic_instance(id, nt->named_object());
	}
    }
}

// Struct Parse::Enclosing_var_comparison.

// Return true if v1 should be considered to be less than v2.

bool
Parse::Enclosing_var_comparison::operator()(const Enclosing_var& v1,
					    const Enclosing_var& v2) const
{
  if (v1.var() == v2.var())
    return false;

  const std::string& n1(v1.var()->name());
  const std::string& n2(v2.var()->name());
  int i = n1.compare(n2);
  if (i < 0)
    return true;
  else if (i > 0)
    return false;

  // If we get here it means that a single nested function refers to
  // two different variables defined in enclosing functions, and both
  // variables have the same name.  I think this is impossible.
  go_unreachable();
}

// Class Parse.

Parse::Parse(Lex* lex, Gogo* gogo)
  : lex_(lex),
    replay_tokens_(NULL),
    counted_reparse_(false),
    replay_index_(0),
    replay_pkg_aliases_(NULL),
    token_(Token::make_invalid_token(Linemap::unknown_location())),
    ungot_(),
    is_erroneous_function_(false),
    gogo_(gogo),
    break_stack_(NULL),
    continue_stack_(NULL),
    enclosing_vars_(),
    shared_enclosing_vars_(NULL),
    iface_terms_(),
    last_iface_terms_()
{
}

// Switch this parser to replay tokens from TOKENS instead of reading
// from the lexer.  Used when re-parsing a generic function instance.

void
Parse::set_replay_tokens(const std::vector<Token>* tokens)
{
  this->replay_tokens_ = tokens;
  this->replay_index_ = 0;
  this->token_ = Token::make_invalid_token(Linemap::unknown_location());
  this->ungot_.clear();

  // Mark that we are re-parsing captured tokens rather than original
  // source, so that Gogo::lookup's package-name last resort is active (see
  // Gogo::enter_reparse).  Balanced in the destructor.
  if (tokens != NULL && !this->counted_reparse_)
    {
      this->gogo_->enter_reparse();
      this->counted_reparse_ = true;
    }
}

Parse::~Parse()
{
  if (this->counted_reparse_)
    this->gogo_->leave_reparse();
}

// Fetch the next token, either from the replay buffer or the lexer.

Token
Parse::lex_next_token()
{
  if (this->replay_tokens_ != NULL)
    {
      if (this->replay_index_ >= this->replay_tokens_->size())
	return Token::make_eof_token(Linemap::unknown_location());
      return (*this->replay_tokens_)[this->replay_index_++];
    }
  return this->lex_->next_token();
}

// Return the current token.

const Token*
Parse::peek_token()
{
  if (!this->ungot_.empty())
    return &this->ungot_.back();
  if (this->token_.is_invalid())
    this->token_ = this->lex_next_token();
  return &this->token_;
}

// Advance to the next token and return it.

const Token*
Parse::advance_token()
{
  if (!this->ungot_.empty())
    {
      this->ungot_.pop_back();
      if (!this->ungot_.empty())
	return &this->ungot_.back();
      if (!this->token_.is_invalid())
	return &this->token_;
    }
  this->token_ = this->lex_next_token();
  return &this->token_;
}

// Push a token back on the input stream.  Tokens form a stack: the most
// recently pushed token is the next one peek_token will return.

void
Parse::unget_token(const Token& token)
{
  this->ungot_.push_back(token);
}

// The location of the current token.

Location
Parse::location()
{
  return this->peek_token()->location();
}

// IdentifierList = identifier { "," identifier } .

void
Parse::identifier_list(Typed_identifier_list* til)
{
  const Token* token = this->peek_token();
  while (true)
    {
      if (!token->is_identifier())
	{
	  go_error_at(this->location(), "expected identifier");
	  return;
	}
      std::string name =
	this->gogo_->pack_hidden_name(token->identifier(),
				      token->is_identifier_exported());
      til->push_back(Typed_identifier(name, NULL, token->location()));
      token = this->advance_token();
      if (!token->is_op(OPERATOR_COMMA))
	return;
      token = this->advance_token();
    }
}

// ExpressionList = Expression { "," Expression } .

// If MAY_BE_COMPOSITE_LIT is true, an expression may be a composite
// literal.

// If MAY_BE_SINK is true, the expressions in the list may be "_".

Expression_list*
Parse::expression_list(Expression* first, bool may_be_sink,
		       bool may_be_composite_lit)
{
  Expression_list* ret = new Expression_list();
  if (first != NULL)
    ret->push_back(first);
  while (true)
    {
      ret->push_back(this->expression(PRECEDENCE_NORMAL, may_be_sink,
				      may_be_composite_lit, NULL, NULL));

      const Token* token = this->peek_token();
      if (!token->is_op(OPERATOR_COMMA))
	return ret;

      // Most expression lists permit a trailing comma.
      Location location = token->location();
      this->advance_token();
      if (!this->expression_may_start_here())
	{
	  this->unget_token(Token::make_operator_token(OPERATOR_COMMA,
						       location));
	  return ret;
	}
    }
}

// QualifiedIdent = [ PackageName "." ] identifier .
// PackageName = identifier .

// This sets *PNAME to the identifier and sets *PPACKAGE to the
// package or NULL if there isn't one.  This returns true on success,
// false on failure in which case it will have emitted an error
// message.

bool
Parse::qualified_ident(std::string* pname, Named_object** ppackage)
{
  const Token* token = this->peek_token();
  if (!token->is_identifier())
    {
      go_error_at(this->location(), "expected identifier");
      return false;
    }

  std::string raw_alias = token->identifier();
  std::string name = token->identifier();
  bool is_exported = token->is_identifier_exported();
  name = this->gogo_->pack_hidden_name(name, is_exported);

  token = this->advance_token();
  if (!token->is_op(OPERATOR_DOT))
    {
      *pname = name;
      *ppackage = NULL;
      return true;
    }

  // While re-parsing a generic instance, resolve a package qualifier through
  // the template's recorded alias->pkgpath map.  This selects the exact
  // package the template was written against, rather than letting an
  // ambiguous by-name lookup pick a different same-named package that
  // happens to be imported elsewhere in this compilation.
  Named_object* package = NULL;
  if (this->replay_pkg_aliases_ != NULL)
    {
      std::map<std::string, std::string>::const_iterator a =
	this->replay_pkg_aliases_->find(raw_alias);
      if (a != this->replay_pkg_aliases_->end())
	{
	  package = this->gogo_->package_no_for_pkgpath(a->second);
	  // Ensure the qualifier alias is registered for usage tracking, so
	  // the note_usage below does not assert (the package's own name may
	  // differ from the alias used here).
	  if (package != NULL)
	    package->package_value()->add_alias(raw_alias,
						Linemap::unknown_location());
	}
    }
  if (package == NULL)
    package = this->gogo_->lookup(name, NULL);
  if (package == NULL || !package->is_package())
    {
      if (package == NULL)
	go_error_at(this->location(), "reference to undefined name %qs",
		    Gogo::message_name(name).c_str());
      else
	go_error_at(this->location(), "expected package");
      // We expect . IDENTIFIER; skip both.
      if (this->advance_token()->is_identifier())
	this->advance_token();
      return false;
    }

  package->package_value()->note_usage(Gogo::unpack_hidden_name(name));

  token = this->advance_token();
  if (!token->is_identifier())
    {
      go_error_at(this->location(), "expected identifier");
      return false;
    }

  name = token->identifier();

  if (name == "_")
    {
      go_error_at(this->location(), "invalid use of %<_%>");
      name = Gogo::erroneous_name();
    }

  if (package->name() == this->gogo_->package_name())
    name = this->gogo_->pack_hidden_name(name,
					 token->is_identifier_exported());

  *pname = name;
  *ppackage = package;

  this->advance_token();

  return true;
}

// Type = TypeName | TypeLit | "(" Type ")" .
// TypeLit =
// 	ArrayType | StructType | PointerType | FunctionType | InterfaceType |
// 	SliceType | MapType | ChannelType .

Type*
Parse::type(bool issue_error)
{
  const Token* token = this->peek_token();
  if (token->is_identifier())
    return this->type_name(issue_error);
  else if (token->is_op(OPERATOR_LSQUARE))
    return this->array_type(false);
  else if (token->is_keyword(KEYWORD_CHAN)
	   || token->is_op(OPERATOR_CHANOP))
    return this->channel_type();
  else if (token->is_keyword(KEYWORD_INTERFACE))
    return this->interface_type(true);
  else if (token->is_keyword(KEYWORD_FUNC))
    {
      Location location = token->location();
      this->advance_token();
      Type* type = this->signature(NULL, location);
      if (type == NULL)
	return Type::make_error_type();
      return type;
    }
  else if (token->is_keyword(KEYWORD_MAP))
    return this->map_type();
  else if (token->is_keyword(KEYWORD_STRUCT))
    return this->struct_type();
  else if (token->is_op(OPERATOR_MULT))
    return this->pointer_type();
  else if (token->is_op(OPERATOR_LPAREN))
    {
      this->advance_token();
      Type* ret = this->type(issue_error);
      if (this->peek_token()->is_op(OPERATOR_RPAREN))
	this->advance_token();
      else
	{
	  if (!ret->is_error_type())
	    go_error_at(this->location(), "expected %<)%>");
	}
      return ret;
    }
  else
    {
      go_error_at(token->location(), "expected type");
      return Type::make_error_type();
    }
}

bool
Parse::type_may_start_here()
{
  const Token* token = this->peek_token();
  return (token->is_identifier()
	  || token->is_op(OPERATOR_LSQUARE)
	  || token->is_op(OPERATOR_CHANOP)
	  || token->is_keyword(KEYWORD_CHAN)
	  || token->is_keyword(KEYWORD_INTERFACE)
	  || token->is_keyword(KEYWORD_FUNC)
	  || token->is_keyword(KEYWORD_MAP)
	  || token->is_keyword(KEYWORD_STRUCT)
	  || token->is_op(OPERATOR_MULT)
	  || token->is_op(OPERATOR_LPAREN));
}

// Generics: opaque replay bindings for FUNCTION-LOCAL types used as generic
// type arguments.  A function-local type is out of scope at the (possibly
// later-pass) point where the generic it is passed to is instantiated, so its
// bare source name would be undefined during that replay.  Instead each such
// type is given a stable synthetic name "$localtypeN" that maps directly back
// to the ORIGINAL Named_object (preserving nominal identity, unlike a package
// alias): type_to_tokens emits "$localtypeN" for any in-function named type,
// and type_name resolves "$localtypeN" back to the original type during replay.
// Global for the (single-package) compilation; the synthetic names are unique
// per Named_object so no scoping/threading is needed.
static std::map<const Named_object*, std::string> replay_local_type_syn;
static std::map<std::string, Named_object*> replay_local_type_obj;

// Generics: stack of the enclosing generic function instantiation's type
// arguments (source-token lists) while replaying its body, so a function-local
// generic type declared in the body can record them (for its reflection name).
static std::vector<std::vector<std::vector<Token> > > enclosing_generic_args_stack;

// Return the stable "$localtypeN" synthetic name for a function-local named
// type, allocating and recording it (both directions) on first use.
static std::string
replay_name_for_local_type(Named_object* no)
{
  std::map<const Named_object*, std::string>::const_iterator p =
    replay_local_type_syn.find(no);
  if (p != replay_local_type_syn.end())
    return p->second;
  char buf[40];
  snprintf(buf, sizeof buf, "$localtype%u",
	   (unsigned) replay_local_type_syn.size());
  std::string syn(buf);
  replay_local_type_syn[no] = syn;
  replay_local_type_obj[syn] = no;
  return syn;
}

// TypeName = QualifiedIdent .

// If MAY_BE_NIL is true, then an identifier with the value of the
// predefined constant nil is accepted, returning the nil type.

Type*
Parse::type_name(bool issue_error)
{
  Location location = this->location();

  std::string name;
  Named_object* package;
  if (!this->qualified_ident(&name, &package))
    return Type::make_error_type();

  // Generics: a "$localtypeN" synthetic name (emitted by type_to_tokens for a
  // function-local type used as a generic type argument) resolves back to the
  // exact original local type object -- its bare source name would be out of
  // scope here (see replay_local_type_obj).
  if (package == NULL)
    {
      std::string bare =
	Gogo::is_hidden_name(name) ? Gogo::unpack_hidden_name(name) : name;
      if (bare.compare(0, 10, "$localtype") == 0)
	{
	  std::map<std::string, Named_object*>::const_iterator lp =
	    replay_local_type_obj.find(bare);
	  if (lp != replay_local_type_obj.end() && lp->second->is_type())
	    return lp->second->type_value();
	}
    }

  // Generics: a "[" after a type name is a type argument list.  If the
  // generic type is already known, instantiate now.  Otherwise it is a
  // forward reference: record a pending instantiation to resolve after
  // parsing.  This also applies while re-parsing an instance during the
  // parse phase (e.g. an instance whose signature names a generic type
  // declared later in the file); such pending instantiations are resolved
  // by the post-parse pass.  It does not apply once parsing is complete,
  // because that pass has already run -- but then every generic type is
  // registered, so the lookup above succeeds and we never reach here.
  if (package == NULL
      && this->peek_token()->is_op(OPERATOR_LSQUARE))
    {
      Generic_function_info* ginfo = this->gogo_->lookup_generic_type(name);
      // While re-parsing an imported template, a bare reference to one of the
      // defining package's own (possibly unexported) generic types is written
      // unqualified; the name was packed with the importing package's pkgpath,
      // so also try the defining package's pkgpath.
      if (ginfo == NULL)
	{
	  Package* ip = this->gogo_->current_instantiation_package();
	  if (ip != NULL)
	    ginfo = this->gogo_->lookup_generic_type(
	      ip->pkgpath() + '.' + Gogo::unpack_hidden_name(name));
	}
      if (ginfo != NULL)
	return this->generic_type_instantiation(ginfo, location);
      if (!this->gogo_->parsing_complete())
	return this->pending_generic_type_instantiation(name, location);
    }

  // Generics: a "[" after a qualified type name "pkg.T" instantiates a
  // generic type imported from another package.
  if (package != NULL
      && this->peek_token()->is_op(OPERATOR_LSQUARE))
    {
      Generic_function_info* ginfo =
	this->gogo_->lookup_generic_type(package->package_value()->pkgpath()
					 + '.' + name);
      if (ginfo != NULL)
	return this->generic_type_instantiation(ginfo, location);
    }

  Named_object* named_object;
  if (package == NULL)
    {
      named_object = NULL;
      // Generics: an unqualified name in an instantiated template body belongs
      // to the template's defining package, not the package being compiled.
      // Resolve it against the defining (instantiation) package first, so that
      // e.g. metricdata's "type Temporality" in a re-parsed metricdata.Sum
      // template is not shadowed by a same-named "func Temporality" in the
      // importing package.  Fall back to the ordinary lookup otherwise.
      // Never redirect an inference marker ("$infermarkerK"): markers are
      // compiler-internal types that always belong to the current compilation
      // package's bindings, and the defining package may hold an unrelated
      // same-named forward declaration that would shadow the real marker and
      // break inference of a nested generic call.
      if (this->replay_tokens_ != NULL
	  && name.compare(0, 12, "$infermarker") != 0)
	{
	  // Walk the whole instantiation-package stack (innermost first).  A
	  // bare type-argument name captured in an OUTER template's body (e.g.
	  // resolver.Address as an argument to iter.Seq2 inside a method of
	  // resolver.AddressMapV2[T]) must resolve against that outer package
	  // even though a nested generic (iter) is currently on top of stack.
	  named_object = this->lookup_type_in_instantiation_packages(name);
	}
      if (named_object == NULL)
	named_object = this->gogo_->lookup(name, NULL);
    }
  else
    {
      named_object = package->package_value()->lookup(name);
      if (named_object == NULL
	  && issue_error
	  && package->name() != this->gogo_->package_name())
	{
	  // Check whether the name is there but hidden.
	  std::string s = ('.' + package->package_value()->pkgpath()
			   + '.' + name);
	  named_object = package->package_value()->lookup(s);
	  if (named_object != NULL)
	    {
	      Package* p = package->package_value();
	      const std::string& packname(p->package_name());
	      go_error_at(location,
			  "invalid reference to hidden type %<%s.%s%>",
			  Gogo::message_name(packname).c_str(),
			  Gogo::message_name(name).c_str());
	      issue_error = false;
	    }
	}
    }

  // Generics: while re-parsing an instantiated template, a predeclared
  // named type (such as "error") used only via a type argument may not be
  // connected to its universe definition by the global name-resolution
  // pass, leaving it an unresolved unknown.  Resolve it directly from the
  // global bindings here.  Restricted to re-parse (replay) mode so normal
  // forward references are unaffected.
  if (named_object == NULL
      && package == NULL
      && this->replay_tokens_ != NULL)
    {
      Named_object* g =
	this->gogo_->lookup_global(Gogo::unpack_hidden_name(name).c_str());
      if (g != NULL && (g->is_type() || g->is_type_declaration()))
	named_object = g;
    }

  // The predeclared "nil" is valid as a type in a type-switch case
  // ("case nil:").  A generic function/method body is captured as tokens and
  // only ever parsed during instantiation, by which point "nil" has resolved
  // to the universe constant rather than an unknown forward reference.
  // Return a forward declaration wrapping that constant -- the representation
  // the type-switch lowering recognizes (is_nil_constant_as_type) -- so the
  // case lowers to a nil comparison rather than a (basic, unnamed) type
  // descriptor.
  if (package == NULL
      && named_object != NULL
      && named_object->is_const()
      && Gogo::unpack_hidden_name(name) == "nil")
    {
      // Build the forward declaration over the unknown first (it resolves the
      // object now, and an unknown with no real object yet resolves to
      // itself), then point the unknown at the nil constant so
      // is_nil_constant_as_type sees through it.
      Named_object* u = Named_object::make_unknown_name("nil", location);
      Type* fwd = Type::make_forward_declaration(u);
      u->unknown_value()->set_real_named_object(named_object);
      return fwd;
    }

  bool ok = true;
  if (named_object == NULL)
    {
      if (package == NULL)
	named_object = this->gogo_->add_unknown_name(name, location);
      else
	{
	  if (issue_error)
	    {
	      const std::string& packname(
		package->package_value()->package_name());
	      go_error_at(location, "reference to undefined identifier %<%s.%s%>",
			  Gogo::message_name(packname).c_str(),
			  Gogo::message_name(name).c_str());
	    }
	  issue_error = false;
	  ok = false;
	}
    }
  else if (named_object->is_type())
    {
      // Generics: while re-parsing an imported generic template (replay),
      // a qualified reference names a type from one of the template's
      // defining-package imports.  Such a package is imported by the compiler
      // to instantiate the template (see go_import_generics "genimports"),
      // but its types can be marked not-visible if they were first created as
      // inlined references in another package's export data (import.cc
      // deliberately does not change an existing type's visibility).  The
      // template was valid in its defining package, so accept the type here
      // regardless of the visibility flag; the ordinary (non-replay) check is
      // preserved.
      if (!named_object->type_value()->is_visible()
	  && this->replay_tokens_ == NULL)
	ok = false;
    }
  else if (named_object->is_unknown() || named_object->is_type_declaration())
    ;
  else
    ok = false;

  if (!ok)
    {
      if (issue_error)
	go_error_at(location, "expected type");
      return Type::make_error_type();
    }

  if (named_object->is_type())
    return named_object->type_value();
  else if (named_object->is_unknown() || named_object->is_type_declaration())
    return Type::make_forward_declaration(named_object);
  else
    go_unreachable();
}

// ArrayType = "[" [ ArrayLength ] "]" ElementType .
// ArrayLength = Expression .
// ElementType = CompleteType .

Type*
Parse::array_type(bool may_use_ellipsis)
{
  go_assert(this->peek_token()->is_op(OPERATOR_LSQUARE));
  const Token* token = this->advance_token();

  Expression* length = NULL;
  if (token->is_op(OPERATOR_RSQUARE))
    this->advance_token();
  else
    {
      if (!token->is_op(OPERATOR_ELLIPSIS))
	length = this->expression(PRECEDENCE_NORMAL, false, true, NULL, NULL);
      else if (may_use_ellipsis)
	{
	  // An ellipsis is used in composite literals to represent a
	  // fixed array of the size of the number of elements.  We
	  // use a length of nil to represent this, and change the
	  // length when parsing the composite literal.
	  length = Expression::make_nil(this->location());
	  this->advance_token();
	}
      else
	{
	  go_error_at(this->location(),
		      "use of %<[...]%> outside of array literal");
	  length = Expression::make_error(this->location());
	  this->advance_token();
	}
      if (!this->peek_token()->is_op(OPERATOR_RSQUARE))
	{
	  go_error_at(this->location(), "expected %<]%>");
	  return Type::make_error_type();
	}
      this->advance_token();
    }

  Type* element_type = this->type();
  if (element_type->is_error_type())
    return Type::make_error_type();

  return Type::make_array_type(element_type, length);
}

// MapType = "map" "[" KeyType "]" ValueType .
// KeyType = CompleteType .
// ValueType = CompleteType .

Type*
Parse::map_type()
{
  Location location = this->location();
  go_assert(this->peek_token()->is_keyword(KEYWORD_MAP));
  if (!this->advance_token()->is_op(OPERATOR_LSQUARE))
    {
      go_error_at(this->location(), "expected %<[%>");
      return Type::make_error_type();
    }
  this->advance_token();

  Type* key_type = this->type();

  if (!this->peek_token()->is_op(OPERATOR_RSQUARE))
    {
      go_error_at(this->location(), "expected %<]%>");
      return Type::make_error_type();
    }
  this->advance_token();

  Type* value_type = this->type();

  if (key_type->is_error_type() || value_type->is_error_type())
    return Type::make_error_type();

  return Type::make_map_type(key_type, value_type, location);
}

// StructType     = "struct" "{" { FieldDecl ";" } "}" .

Type*
Parse::struct_type()
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_STRUCT));
  Location location = this->location();
  if (!this->advance_token()->is_op(OPERATOR_LCURLY))
    {
      Location token_loc = this->location();
      if (this->peek_token()->is_op(OPERATOR_SEMICOLON)
	  && this->advance_token()->is_op(OPERATOR_LCURLY))
	go_error_at(token_loc, "unexpected semicolon or newline before %<{%>");
      else
	{
	  go_error_at(this->location(), "expected %<{%>");
	  return Type::make_error_type();
	}
    }
  this->advance_token();

  Struct_field_list* sfl = new Struct_field_list;
  while (!this->peek_token()->is_op(OPERATOR_RCURLY))
    {
      this->field_decl(sfl);
      if (this->peek_token()->is_op(OPERATOR_SEMICOLON))
	this->advance_token();
      else if (!this->peek_token()->is_op(OPERATOR_RCURLY))
	{
	  go_error_at(this->location(), "expected %<;%> or %<}%> or newline");
	  if (!this->skip_past_error(OPERATOR_RCURLY))
	    return Type::make_error_type();
	}
    }
  this->advance_token();

  for (Struct_field_list::const_iterator pi = sfl->begin();
       pi != sfl->end();
       ++pi)
    {
      if (pi->type()->is_error_type())
	return pi->type();
      for (Struct_field_list::const_iterator pj = pi + 1;
	   pj != sfl->end();
	   ++pj)
	{
	  if (pi->field_name() == pj->field_name()
	      && !Gogo::is_sink_name(pi->field_name()))
	    go_error_at(pi->location(), "duplicate field name %<%s%>",
			Gogo::message_name(pi->field_name()).c_str());
	}
    }

  return Type::make_struct_type(sfl, location);
}

// FieldDecl = (IdentifierList CompleteType | TypeName) [ Tag ] .
// Tag = string_lit .

void
Parse::field_decl(Struct_field_list* sfl)
{
  const Token* token = this->peek_token();
  Location location = token->location();
  bool is_anonymous;
  bool is_anonymous_pointer;
  if (token->is_op(OPERATOR_MULT))
    {
      is_anonymous = true;
      is_anonymous_pointer = true;
    }
  else if (token->is_identifier())
    {
      std::string id = token->identifier();
      bool is_id_exported = token->is_identifier_exported();
      Location id_location = token->location();
      token = this->advance_token();
      is_anonymous = (token->is_op(OPERATOR_SEMICOLON)
		      || token->is_op(OPERATOR_RCURLY)
		      || token->is_op(OPERATOR_DOT)
		      || token->is_string());
      // Generics: an embedded generic type "B[...]" -- ID names a generic
      // type and is followed by "[" -- is an anonymous (embedded) field,
      // not a named field of an array type.  While re-parsing an imported
      // template, the embedded type may be one of the defining package's own
      // generic types, registered under that package's pkgpath.
      if (token->is_op(OPERATOR_LSQUARE))
	{
	  bool is_generic =
	    (this->gogo_->lookup_generic_type(
	       this->gogo_->pack_hidden_name(id, is_id_exported)) != NULL);
	  if (!is_generic)
	    {
	      Package* ip = this->gogo_->current_instantiation_package();
	      if (ip != NULL)
		is_generic = (this->gogo_->lookup_generic_type(
				ip->pkgpath() + '.' + id) != NULL);
	    }
	  if (!is_generic)
	    {
	      // The generic type may be a forward reference (declared later in
	      // the file), so the registration lookups above fail.
	      // Distinguish an embedded generic type "T[args]" -- where the
	      // whole "id[...]" is the type and a field terminator follows the
	      // "]" -- from a named field of array type "name [N]Elem", where a
	      // type follows the "]".  Scan the balanced brackets and look at
	      // what comes next, then restore the token stream.
	      std::vector<Token> scanned;
	      int depth = 0;
	      while (true)
		{
		  const Token* t = this->peek_token();
		  if (t->is_eof())
		    break;
		  scanned.push_back(*t);
		  bool close = false;
		  if (t->is_op(OPERATOR_LSQUARE))
		    ++depth;
		  else if (t->is_op(OPERATOR_RSQUARE))
		    {
		      --depth;
		      if (depth == 0)
			close = true;
		    }
		  this->advance_token();
		  if (close)
		    break;
		}
	      const Token* after = this->peek_token();
	      if (after->is_op(OPERATOR_SEMICOLON)
		  || after->is_op(OPERATOR_RCURLY)
		  || after->is_string()
		  || after->is_eof())
		is_generic = true;
	      for (std::vector<Token>::reverse_iterator ri = scanned.rbegin();
		   ri != scanned.rend(); ++ri)
		this->unget_token(*ri);
	    }
	  if (is_generic)
	    is_anonymous = true;
	}
      is_anonymous_pointer = false;
      this->unget_token(Token::make_identifier_token(id, is_id_exported,
						     id_location));
    }
  else
    {
      go_error_at(this->location(), "expected field name");
      this->gogo_->mark_locals_used();
      while (!token->is_op(OPERATOR_SEMICOLON)
	     && !token->is_op(OPERATOR_RCURLY)
	     && !token->is_eof())
	token = this->advance_token();
      return;
    }

  if (is_anonymous)
    {
      if (is_anonymous_pointer)
	{
	  this->advance_token();
	  if (!this->peek_token()->is_identifier())
	    {
	      go_error_at(this->location(), "expected field name");
	      this->gogo_->mark_locals_used();
	      while (!token->is_op(OPERATOR_SEMICOLON)
		     && !token->is_op(OPERATOR_RCURLY)
		     && !token->is_eof())
		token = this->advance_token();
	      return;
	    }
	}
      Type* type = this->type_name(true);

      std::string tag;
      if (this->peek_token()->is_string())
	{
	  tag = this->peek_token()->string_value();
	  this->advance_token();
	}

      if (!type->is_error_type())
	{
	  if (is_anonymous_pointer)
	    type = Type::make_pointer_type(type);
	  sfl->push_back(Struct_field(Typed_identifier("", type, location)));
	  if (!tag.empty())
	    sfl->back().set_tag(tag);
	}
    }
  else
    {
      Typed_identifier_list til;
      while (true)
	{
	  token = this->peek_token();
	  if (!token->is_identifier())
	    {
	      go_error_at(this->location(), "expected identifier");
	      return;
	    }
	  std::string name =
	    (Gogo::is_hidden_name(token->identifier())
	     ? token->identifier()
	     : this->gogo_->pack_hidden_name_for_field(
		 token->identifier(), token->is_identifier_exported()));
	  til.push_back(Typed_identifier(name, NULL, token->location()));
	  if (!this->advance_token()->is_op(OPERATOR_COMMA))
	    break;
	  this->advance_token();
	}

      Type* type = this->type();

      std::string tag;
      if (this->peek_token()->is_string())
	{
	  tag = this->peek_token()->string_value();
	  this->advance_token();
	}

      for (Typed_identifier_list::iterator p = til.begin();
	   p != til.end();
	   ++p)
	{
	  p->set_type(type);
	  sfl->push_back(Struct_field(*p));
	  if (!tag.empty())
	    sfl->back().set_tag(tag);
	}
    }
}

// PointerType = "*" Type .

Type*
Parse::pointer_type()
{
  go_assert(this->peek_token()->is_op(OPERATOR_MULT));
  this->advance_token();
  Type* type = this->type();
  if (type->is_error_type())
    return type;
  return Type::make_pointer_type(type);
}

// ChannelType   = Channel | SendChannel | RecvChannel .
// Channel       = "chan" ElementType .
// SendChannel   = "chan" "<-" ElementType .
// RecvChannel   = "<-" "chan" ElementType .

Type*
Parse::channel_type()
{
  const Token* token = this->peek_token();
  bool send = true;
  bool receive = true;
  if (token->is_op(OPERATOR_CHANOP))
    {
      if (!this->advance_token()->is_keyword(KEYWORD_CHAN))
	{
	  go_error_at(this->location(), "expected %<chan%>");
	  return Type::make_error_type();
	}
      send = false;
      this->advance_token();
    }
  else
    {
      go_assert(token->is_keyword(KEYWORD_CHAN));
      if (this->advance_token()->is_op(OPERATOR_CHANOP))
	{
	  receive = false;
	  this->advance_token();
	}
    }

  // Better error messages for the common error of omitting the
  // channel element type.
  if (!this->type_may_start_here())
    {
      token = this->peek_token();
      if (token->is_op(OPERATOR_RCURLY))
	go_error_at(this->location(), "unexpected %<}%> in channel type");
      else if (token->is_op(OPERATOR_RPAREN))
	go_error_at(this->location(), "unexpected %<)%> in channel type");
      else if (token->is_op(OPERATOR_COMMA))
	go_error_at(this->location(), "unexpected comma in channel type");
      else
	go_error_at(this->location(), "expected channel element type");
      return Type::make_error_type();
    }

  Type* element_type = this->type();
  return Type::make_channel_type(send, receive, element_type);
}

// Give an error for a duplicate parameter or receiver name.

void
Parse::check_signature_names(const Typed_identifier_list* params,
			     Parse::Names* names)
{
  for (Typed_identifier_list::const_iterator p = params->begin();
       p != params->end();
       ++p)
    {
      if (p->name().empty() || Gogo::is_sink_name(p->name()))
	continue;
      std::pair<std::string, const Typed_identifier*> val =
	std::make_pair(p->name(), &*p);
      std::pair<Parse::Names::iterator, bool> ins = names->insert(val);
      if (!ins.second)
	{
	  go_error_at(p->location(), "redefinition of %qs",
		      Gogo::message_name(p->name()).c_str());
	  go_inform(ins.first->second->location(),
		    "previous definition of %qs was here",
		    Gogo::message_name(p->name()).c_str());
	}
    }
}

// Signature      = Parameters [ Result ] .

// RECEIVER is the receiver if there is one, or NULL.  LOCATION is the
// location of the start of the type.

// This returns NULL on a parse error.

Function_type*
Parse::signature(Typed_identifier* receiver, Location location)
{
  bool is_varargs = false;
  Typed_identifier_list* params;
  bool params_ok = this->parameters(&params, &is_varargs);

  Typed_identifier_list* results = NULL;
  if (this->peek_token()->is_op(OPERATOR_LPAREN)
      || this->type_may_start_here())
    {
      if (!this->result(&results))
	return NULL;
    }

  if (!params_ok)
    return NULL;

  Parse::Names names;
  if (receiver != NULL)
    names[receiver->name()] = receiver;
  if (params != NULL)
    this->check_signature_names(params, &names);
  if (results != NULL)
    this->check_signature_names(results, &names);

  Function_type* ret = Type::make_function_type(receiver, params, results,
						location);
  if (is_varargs)
    ret->set_is_varargs();
  return ret;
}

// Parameters     = "(" [ ParameterList [ "," ] ] ")" .

// This returns false on a parse error.

bool
Parse::parameters(Typed_identifier_list** pparams, bool* is_varargs)
{
  *pparams = NULL;

  if (!this->peek_token()->is_op(OPERATOR_LPAREN))
    {
      go_error_at(this->location(), "expected %<(%>");
      return false;
    }

  Typed_identifier_list* params = NULL;
  bool saw_error = false;

  const Token* token = this->advance_token();
  if (!token->is_op(OPERATOR_RPAREN))
    {
      params = this->parameter_list(is_varargs);
      if (params == NULL)
	saw_error = true;
      token = this->peek_token();
    }

  // The optional trailing comma is picked up in parameter_list.

  if (!token->is_op(OPERATOR_RPAREN))
    {
      go_error_at(this->location(), "expected %<)%>");
      return false;
    }
  this->advance_token();

  if (saw_error)
    return false;

  *pparams = params;
  return true;
}

// ParameterList  = ParameterDecl { "," ParameterDecl } .

// This sets *IS_VARARGS if the list ends with an ellipsis.
// IS_VARARGS will be NULL if varargs are not permitted.

// We pick up an optional trailing comma.

// This returns NULL if some error is seen.

Typed_identifier_list*
Parse::parameter_list(bool* is_varargs)
{
  Location location = this->location();
  Typed_identifier_list* ret = new Typed_identifier_list();

  bool saw_error = false;

  // If we see an identifier and then a comma, then we don't know
  // whether we are looking at a list of identifiers followed by a
  // type, or a list of types given by name.  We have to do an
  // arbitrary lookahead to figure it out.

  bool parameters_have_names;
  const Token* token = this->peek_token();
  if (!token->is_identifier())
    {
      // This must be a type which starts with something like '*'.
      parameters_have_names = false;
    }
  else
    {
      std::string name = token->identifier();
      bool is_exported = token->is_identifier_exported();
      Location id_location = token->location();
      token = this->advance_token();
      if (!token->is_op(OPERATOR_COMMA))
	{
	  if (token->is_op(OPERATOR_DOT))
	    {
	      // This is a qualified identifier, which must turn out
	      // to be a type.
	      parameters_have_names = false;
	    }
	  else if (token->is_op(OPERATOR_RPAREN))
	    {
	      // A single identifier followed by a parenthesis must be
	      // a type name.
	      parameters_have_names = false;
	    }
	  else if (token->is_op(OPERATOR_LSQUARE)
		   && this->name_is_generic_type(name, is_exported))
	    {
	      // Generics: "Foo[...]" where Foo is a generic type is an
	      // unnamed parameter of a generic-type instantiation, not a
	      // parameter name followed by an array type.
	      parameters_have_names = false;
	    }
	  else
	    {
	      // An identifier followed by something other than a
	      // comma or a dot or a right parenthesis must be a
	      // parameter name followed by a type.
	      parameters_have_names = true;
	    }

	  this->unget_token(Token::make_identifier_token(name, is_exported,
							 id_location));
	}
      else
	{
	  // An identifier followed by a comma may be the first in a
	  // list of parameter names followed by a type, or it may be
	  // the first in a list of types without parameter names.  To
	  // find out we gather as many identifiers separated by
	  // commas as we can.
	  std::string id_name = this->gogo_->pack_hidden_name(name,
							      is_exported);
	  ret->push_back(Typed_identifier(id_name, NULL, id_location));
	  bool just_saw_comma = true;
	  while (this->advance_token()->is_identifier())
	    {
	      name = this->peek_token()->identifier();
	      is_exported = this->peek_token()->is_identifier_exported();
	      id_location = this->peek_token()->location();
	      id_name = this->gogo_->pack_hidden_name(name, is_exported);
	      ret->push_back(Typed_identifier(id_name, NULL, id_location));
	      if (!this->advance_token()->is_op(OPERATOR_COMMA))
		{
		  just_saw_comma = false;
		  break;
		}
	    }

	  if (just_saw_comma)
	    {
	      // We saw ID1 "," ID2 "," followed by something which
	      // was not an identifier.  We must be seeing the start
	      // of a type, and ID1 and ID2 must be types, and the
	      // parameters don't have names.
	      parameters_have_names = false;
	    }
	  else if (this->peek_token()->is_op(OPERATOR_RPAREN))
	    {
	      // We saw ID1 "," ID2 ")".  ID1 and ID2 must be types,
	      // and the parameters don't have names.
	      parameters_have_names = false;
	    }
	  else if (this->peek_token()->is_op(OPERATOR_DOT))
	    {
	      // We saw ID1 "," ID2 ".".  ID2 must be a package name,
	      // ID1 must be a type, and the parameters don't have
	      // names.
	      parameters_have_names = false;
	      this->unget_token(Token::make_identifier_token(name, is_exported,
							     id_location));
	      ret->pop_back();
	      just_saw_comma = true;
	    }
	  else
	    {
	      // We saw ID1 "," ID2 followed by something other than
	      // ",", ".", or ")".  We must be looking at the start of
	      // a type, and ID1 and ID2 must be parameter names.
	      parameters_have_names = true;
	    }

	  if (parameters_have_names)
	    {
	      go_assert(!just_saw_comma);
	      // We have just seen ID1, ID2 xxx.
	      Type* type;
	      if (!this->peek_token()->is_op(OPERATOR_ELLIPSIS))
		type = this->type();
	      else
		{
		  go_error_at(this->location(),
			      "%<...%> only permits one name");
		  saw_error = true;
		  this->advance_token();
		  type = this->type();
		}
	      for (size_t i = 0; i < ret->size(); ++i)
		ret->set_type(i, type);
	      if (!this->peek_token()->is_op(OPERATOR_COMMA))
		return saw_error ? NULL : ret;
	      if (this->advance_token()->is_op(OPERATOR_RPAREN))
		return saw_error ? NULL : ret;
	    }
	  else
	    {
	      Typed_identifier_list* tret = new Typed_identifier_list();
	      for (Typed_identifier_list::const_iterator p = ret->begin();
		   p != ret->end();
		   ++p)
		{
		  Named_object* no = this->gogo_->lookup(p->name(), NULL);
		  // Generics: while re-parsing an instantiated template
		  // (replay), an unnamed parameter/result written as a bare
		  // predeclared type name (e.g. the "rune" and "int" in a
		  // "func(S) (rune, int)" parameter) is not connected to its
		  // universe definition by the global name-resolution pass, so
		  // it would otherwise become an unresolved unknown.  Resolve
		  // it directly from the global bindings here, mirroring the
		  // fallback in type_name.  Restricted to replay mode so normal
		  // forward references are unaffected.
		  if (no == NULL && this->replay_tokens_ != NULL)
		    {
		      Named_object* g = this->gogo_->lookup_global(
			Gogo::unpack_hidden_name(p->name()).c_str());
		      if (g != NULL
			  && (g->is_type() || g->is_type_declaration()))
			no = g;
		    }
		  // Generics: the bare type name may belong to a template whose
		  // instantiation is in progress (e.g. an unnamed parameter type
		  // "Address" in "func(Address, T) bool" while instantiating
		  // iter.Seq2[resolver.Address, T]); resolve it against the
		  // instantiation-package stack, as type_name does.
		  if (no == NULL && this->replay_tokens_ != NULL)
		    no = this->lookup_type_in_instantiation_packages(p->name());
		  Type* type;
		  if (no == NULL)
		    no = this->gogo_->add_unknown_name(p->name(),
						       p->location());

		  if (no->is_type())
		    type = no->type_value();
		  else if (no->is_unknown() || no->is_type_declaration())
		    type = Type::make_forward_declaration(no);
		  else
		    {
		      go_error_at(p->location(), "expected %<%s%> to be a type",
				  Gogo::message_name(p->name()).c_str());
		      saw_error = true;
		      type = Type::make_error_type();
		    }
		  tret->push_back(Typed_identifier("", type, p->location()));
		}
	      delete ret;
	      ret = tret;
	      if (!just_saw_comma
		  || this->peek_token()->is_op(OPERATOR_RPAREN))
		return saw_error ? NULL : ret;
	    }
	}
    }

  bool mix_error = false;
  this->parameter_decl(parameters_have_names, ret, is_varargs, &mix_error,
		       &saw_error);
  while (this->peek_token()->is_op(OPERATOR_COMMA))
    {
      if (this->advance_token()->is_op(OPERATOR_RPAREN))
	break;
      if (is_varargs != NULL && *is_varargs)
	{
	  go_error_at(this->location(), "%<...%> must be last parameter");
	  saw_error = true;
	}
      this->parameter_decl(parameters_have_names, ret, is_varargs, &mix_error,
			   &saw_error);
    }
  if (mix_error)
    {
      go_error_at(location, "mixed named and unnamed function parameters");
      saw_error = true;
    }
  if (saw_error)
    {
      delete ret;
      return NULL;
    }
  return ret;
}

// ParameterDecl  = [ IdentifierList ] [ "..." ] Type .

void
Parse::parameter_decl(bool parameters_have_names,
		      Typed_identifier_list* til,
		      bool* is_varargs,
		      bool* mix_error,
		      bool* saw_error)
{
  if (!parameters_have_names)
    {
      Type* type;
      Location location = this->location();
      if (!this->peek_token()->is_identifier())
	{
	  if (!this->peek_token()->is_op(OPERATOR_ELLIPSIS))
	    type = this->type();
	  else
	    {
	      if (is_varargs == NULL)
		go_error_at(this->location(), "invalid use of %<...%>");
	      else
		*is_varargs = true;
	      this->advance_token();
	      if (is_varargs == NULL
		  && this->peek_token()->is_op(OPERATOR_RPAREN))
		type = Type::make_error_type();
	      else
		{
		  Type* element_type = this->type();
		  type = Type::make_array_type(element_type, NULL);
		}
	    }
	}
      else
	{
	  type = this->type_name(false);
	  if (type->is_error_type()
	      || (!this->peek_token()->is_op(OPERATOR_COMMA)
		  && !this->peek_token()->is_op(OPERATOR_RPAREN)))
	    {
	      *mix_error = true;
	      while (!this->peek_token()->is_op(OPERATOR_COMMA)
		     && !this->peek_token()->is_op(OPERATOR_RPAREN)
                     && !this->peek_token()->is_eof())
		this->advance_token();
	    }
	}
      if (!type->is_error_type())
	til->push_back(Typed_identifier("", type, location));
      else
	*saw_error = true;
    }
  else
    {
      size_t orig_count = til->size();
      if (this->peek_token()->is_identifier())
	this->identifier_list(til);
      else
	*mix_error = true;
      size_t new_count = til->size();

      Type* type;
      if (!this->peek_token()->is_op(OPERATOR_ELLIPSIS))
	type = this->type();
      else
	{
	  if (is_varargs == NULL)
	    {
	      go_error_at(this->location(), "invalid use of %<...%>");
	      *saw_error = true;
	    }
	  else if (new_count > orig_count + 1)
	    {
	      go_error_at(this->location(), "%<...%> only permits one name");
	      *saw_error = true;
	    }
	  else
	    *is_varargs = true;
	  this->advance_token();
	  Type* element_type = this->type();
	  type = Type::make_array_type(element_type, NULL);
	}
      for (size_t i = orig_count; i < new_count; ++i)
	til->set_type(i, type);
    }
}

// Result         = Parameters | Type .

// This returns false on a parse error.

bool
Parse::result(Typed_identifier_list** presults)
{
  if (this->peek_token()->is_op(OPERATOR_LPAREN))
    return this->parameters(presults, NULL);
  else
    {
      Location location = this->location();
      Type* type = this->type();
      if (type->is_error_type())
	{
	  *presults = NULL;
	  return false;
	}
      Typed_identifier_list* til = new Typed_identifier_list();
      til->push_back(Typed_identifier("", type, location));
      *presults = til;
      return true;
    }
}

// Block = "{" [ StatementList ] "}" .

// Returns the location of the closing brace.

Location
Parse::block()
{
  if (!this->peek_token()->is_op(OPERATOR_LCURLY))
    {
      Location loc = this->location();
      if (this->peek_token()->is_op(OPERATOR_SEMICOLON)
	  && this->advance_token()->is_op(OPERATOR_LCURLY))
	go_error_at(loc, "unexpected semicolon or newline before %<{%>");
      else
	{
	  go_error_at(this->location(), "expected %<{%>");
	  return Linemap::unknown_location();
	}
    }

  const Token* token = this->advance_token();

  if (!token->is_op(OPERATOR_RCURLY))
    {
      this->statement_list();
      token = this->peek_token();
      if (!token->is_op(OPERATOR_RCURLY))
	{
	  if (!token->is_eof() || !saw_errors())
	    go_error_at(this->location(), "expected %<}%>");

	  this->gogo_->mark_locals_used();

	  // Skip ahead to the end of the block, in hopes of avoiding
	  // lots of meaningless errors.
	  Location ret = token->location();
	  int nest = 0;
	  while (!token->is_eof())
	    {
	      if (token->is_op(OPERATOR_LCURLY))
		++nest;
	      else if (token->is_op(OPERATOR_RCURLY))
		{
		  --nest;
		  if (nest < 0)
		    {
		      this->advance_token();
		      break;
		    }
		}
	      token = this->advance_token();
	      ret = token->location();
	    }
	  return ret;
	}
    }

  Location ret = token->location();
  this->advance_token();
  return ret;
}

// Generics: the type-set elements of each named constraint interface,
// e.g. "type Ordered interface { ~int | ~string }" maps its (packed)
// name "Ordered" to the elements [ "~int", "~string" ].  Used to enforce
// named type-set constraints by expanding them to their elements.
static std::map<std::string, std::vector<std::vector<Token> > >
  named_constraint_type_sets;

// InterfaceType      = "interface" "{" [ MethodSpecList ] "}" .
// MethodSpecList     = MethodSpec { ";" MethodSpec } [ ";" ] .

Type*
Parse::interface_type(bool record)
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_INTERFACE));
  Location location = this->location();

  // Generics: collect this interface's constraint type-set elements,
  // saving and restoring any outer interface's accumulator for nesting.
  std::vector<std::vector<Token> > saved_iface_terms;
  saved_iface_terms.swap(this->iface_terms_);

  if (!this->advance_token()->is_op(OPERATOR_LCURLY))
    {
      Location token_loc = this->location();
      if (this->peek_token()->is_op(OPERATOR_SEMICOLON)
	  && this->advance_token()->is_op(OPERATOR_LCURLY))
	go_error_at(token_loc, "unexpected semicolon or newline before %<{%>");
      else
	{
	  go_error_at(this->location(), "expected %<{%>");
	  return Type::make_error_type();
	}
    }
  this->advance_token();

  Typed_identifier_list* methods = new Typed_identifier_list();
  if (!this->peek_token()->is_op(OPERATOR_RCURLY))
    {
      this->method_spec(methods);
      while (this->peek_token()->is_op(OPERATOR_SEMICOLON))
	{
	  if (this->advance_token()->is_op(OPERATOR_RCURLY))
	    break;
	  this->method_spec(methods);
	}
      if (!this->peek_token()->is_op(OPERATOR_RCURLY))
	{
	  go_error_at(this->location(), "expected %<}%>");
	  while (!this->advance_token()->is_op(OPERATOR_RCURLY))
	    {
	      if (this->peek_token()->is_eof())
		return Type::make_error_type();
	    }
	}
    }
  this->advance_token();

  if (methods->empty())
    {
      delete methods;
      methods = NULL;
    }

  Interface_type* ret;
  if (methods == NULL)
    ret = Type::make_empty_interface_type(location);
  else
    ret = Type::make_interface_type(methods, location);
  if (record)
    this->gogo_->record_interface_type(ret);

  // Hand this interface's collected type-set elements to type_spec, and
  // restore the enclosing interface's accumulator.
  this->last_iface_terms_.swap(this->iface_terms_);
  this->iface_terms_.swap(saved_iface_terms);
  return ret;
}

// MethodSpec         = MethodName Signature | InterfaceTypeName .
// MethodName         = identifier .
// InterfaceTypeName  = TypeName .

void
Parse::method_spec(Typed_identifier_list* methods)
{
  const Token* token = this->peek_token();

  // Generics: a constraint type element that starts with "~" or with a
  // non-identifier type (e.g. "~int", "[]byte").  Record each element's
  // tokens so a named type-set constraint can be enforced later.
  if (token->is_op(OPERATOR_TILDE)
      || (!token->is_identifier() && this->type_may_start_here()))
    {
      std::vector<Token> el;
      this->capture_constraint_element(&el);
      this->iface_terms_.push_back(el);
      while (this->peek_token()->is_op(OPERATOR_OR))
	{
	  this->advance_token();
	  std::vector<Token> e2;
	  this->capture_constraint_element(&e2);
	  this->iface_terms_.push_back(e2);
	}
      return;
    }

  if (!token->is_identifier())
    {
      go_error_at(this->location(), "expected identifier");
      return;
    }

  std::string name = token->identifier();
  bool is_exported = token->is_identifier_exported();
  Location location = token->location();

  if (this->advance_token()->is_op(OPERATOR_LPAREN))
    {
      // This is a MethodName.
      if (name == "_")
	go_error_at(this->location(),
                    "methods must have a unique non-blank name");
      // Generics: an unexported interface method belongs to the package that
      // declared the interface.  While re-parsing an imported generic template
      // (e.g. a type assertion "x.(iface[T])" whose iface has an unexported
      // method), pack the method name with the template's defining package's
      // pkgpath -- as the concrete type's method was packed when its own
      // package was compiled -- so the interface method matches.  Outside
      // instantiation this is identical to pack_hidden_name.
      name = this->gogo_->pack_hidden_name_for_field(name, is_exported);
      Type* type = this->signature(NULL, location);
      if (type == NULL)
	return;
      methods->push_back(Typed_identifier(name, type, location));
    }
  else
    {
      this->unget_token(Token::make_identifier_token(name, is_exported,
						     location));
      Type* type = this->type_name(false);

      // Generics: a union constraint whose first term is a type name,
      // e.g. "int | ~float64".  Record each term so a named type-set
      // constraint can be enforced later.  The first term's tokens are
      // reconstructed from its (simple) name; if it was something more
      // complex it will simply fail to resolve at check time and the
      // whole constraint is then skipped, never wrongly rejected.
      if (this->peek_token()->is_op(OPERATOR_OR))
	{
	  std::vector<Token> first;
	  first.push_back(Token::make_identifier_token(name, is_exported,
						       location));
	  this->iface_terms_.push_back(first);
	  while (this->peek_token()->is_op(OPERATOR_OR))
	    {
	      this->advance_token();
	      std::vector<Token> e;
	      this->capture_constraint_element(&e);
	      this->iface_terms_.push_back(e);
	    }
	  return;
	}

      if (type->is_error_type()
	  || (!this->peek_token()->is_op(OPERATOR_SEMICOLON)
	      && !this->peek_token()->is_op(OPERATOR_RCURLY)))
	{
	  if (this->peek_token()->is_op(OPERATOR_COMMA))
	    go_error_at(this->location(),
			"name list not allowed in interface type");
	  else
	    go_error_at(location, "expected signature or type name");
	  this->gogo_->mark_locals_used();
	  token = this->peek_token();
	  while (!token->is_eof()
		 && !token->is_op(OPERATOR_SEMICOLON)
		 && !token->is_op(OPERATOR_RCURLY))
	    token = this->advance_token();
	  return;
	}
      // This must be an interface type, but we can't check that now.
      // We check it and pull out the methods in
      // Interface_type::do_verify.
      methods->push_back(Typed_identifier("", type, location));
    }
}

// Generics: parse and discard a constraint type element, e.g. "~int"
// or "int | ~float64 | ~string".  Constraints are not yet enforced; we
// only need them to parse.

// Capture the tokens of one constraint type-set element: an optional "~"
// followed by a type, stopping at a top-level "|", ",", ";", "}" or "]".

void
Parse::capture_constraint_element(std::vector<Token>* out)
{
  if (this->peek_token()->is_op(OPERATOR_TILDE))
    {
      out->push_back(*this->peek_token());
      this->advance_token();
    }
  int depth = 0;
  while (true)
    {
      const Token* t = this->peek_token();
      if (t->is_eof())
	break;
      if (depth == 0
	  && (t->is_op(OPERATOR_OR) || t->is_op(OPERATOR_COMMA)
	      || t->is_op(OPERATOR_SEMICOLON) || t->is_op(OPERATOR_RCURLY)
	      || t->is_op(OPERATOR_RSQUARE)))
	break;
      if (t->is_op(OPERATOR_LPAREN) || t->is_op(OPERATOR_LSQUARE)
	  || t->is_op(OPERATOR_LCURLY))
	++depth;
      else if (t->is_op(OPERATOR_RPAREN) || t->is_op(OPERATOR_RSQUARE)
	       || t->is_op(OPERATOR_RCURLY))
	--depth;
      out->push_back(*t);
      this->advance_token();
    }
}

void
Parse::skip_constraint_term()
{
  if (this->peek_token()->is_op(OPERATOR_TILDE))
    this->advance_token();
  this->type();
  while (this->peek_token()->is_op(OPERATOR_OR))
    {
      this->advance_token();
      if (this->peek_token()->is_op(OPERATOR_TILDE))
	this->advance_token();
      this->type();
    }
}

// Declaration = ConstDecl | TypeDecl | VarDecl | FunctionDecl | MethodDecl .

void
Parse::declaration()
{
  const Token* token = this->peek_token();
  if (token->is_keyword(KEYWORD_CONST))
    this->const_decl();
  else if (token->is_keyword(KEYWORD_TYPE))
    this->type_decl();
  else if (token->is_keyword(KEYWORD_VAR))
    this->var_decl();
  else if (token->is_keyword(KEYWORD_FUNC))
    this->function_decl();
  else
    {
      go_error_at(this->location(), "expected declaration");
      this->advance_token();
    }
}

bool
Parse::declaration_may_start_here()
{
  const Token* token = this->peek_token();
  return (token->is_keyword(KEYWORD_CONST)
	  || token->is_keyword(KEYWORD_TYPE)
	  || token->is_keyword(KEYWORD_VAR)
	  || token->is_keyword(KEYWORD_FUNC));
}

// Decl<P> = P | "(" [ List<P> ] ")" .

void
Parse::decl(void (Parse::*pfn)())
{
  if (this->peek_token()->is_eof())
    {
      if (!saw_errors())
	go_error_at(this->location(), "unexpected end of file");
      return;
    }

  if (!this->peek_token()->is_op(OPERATOR_LPAREN))
    (this->*pfn)();
  else
    {
      if (this->lex_->get_and_clear_pragmas() != 0)
	go_error_at(this->location(),
		    "ignoring compiler directive before group");
      if (this->lex_->has_embeds())
	{
	  this->lex_->clear_embeds();
	  go_error_at(this->location(),
		      "ignoring %<//go:embed%> comment before group");
	}
      if (!this->advance_token()->is_op(OPERATOR_RPAREN))
	{
	  this->list(pfn, true);
	  if (!this->peek_token()->is_op(OPERATOR_RPAREN))
	    {
	      go_error_at(this->location(), "missing %<)%>");
	      while (!this->advance_token()->is_op(OPERATOR_RPAREN))
		{
		  if (this->peek_token()->is_eof())
		    return;
		}
	    }
	}
      this->advance_token();
    }
}

// List<P> = P { ";" P } [ ";" ] .

// In order to pick up the trailing semicolon we need to know what
// might follow.  This is either a '}' or a ')'.

void
Parse::list(void (Parse::*pfn)(), bool follow_is_paren)
{
  (this->*pfn)();
  Operator follow = follow_is_paren ? OPERATOR_RPAREN : OPERATOR_RCURLY;
  while (this->peek_token()->is_op(OPERATOR_SEMICOLON)
	 || this->peek_token()->is_op(OPERATOR_COMMA))
    {
      if (this->peek_token()->is_op(OPERATOR_COMMA))
	go_error_at(this->location(), "unexpected comma");
      if (this->advance_token()->is_op(follow))
	break;
      (this->*pfn)();
    }
}

// ConstDecl      = "const" ( ConstSpec | "(" { ConstSpec ";" } ")" ) .

void
Parse::const_decl()
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_CONST));
  this->advance_token();

  int iota = 0;
  Type* last_type = NULL;
  Expression_list* last_expr_list = NULL;

  if (!this->peek_token()->is_op(OPERATOR_LPAREN))
    this->const_spec(iota, &last_type, &last_expr_list);
  else
    {
      this->advance_token();
      while (!this->peek_token()->is_op(OPERATOR_RPAREN))
	{
	  this->const_spec(iota, &last_type, &last_expr_list);
	  ++iota;
	  if (this->peek_token()->is_op(OPERATOR_SEMICOLON))
	    this->advance_token();
	  else if (!this->peek_token()->is_op(OPERATOR_RPAREN))
	    {
	      go_error_at(this->location(),
			  "expected %<;%> or %<)%> or newline");
	      if (!this->skip_past_error(OPERATOR_RPAREN))
		return;
	    }
	}
      this->advance_token();
    }

  if (last_expr_list != NULL)
    delete last_expr_list;
}

// ConstSpec = IdentifierList [ [ CompleteType ] "=" ExpressionList ] .

void
Parse::const_spec(int iota, Type** last_type, Expression_list** last_expr_list)
{
  this->check_directives();

  Location loc = this->location();
  Typed_identifier_list til;
  this->identifier_list(&til);

  Type* type = NULL;
  if (this->type_may_start_here())
    {
      type = this->type();
      *last_type = NULL;
      *last_expr_list = NULL;
    }

  Expression_list *expr_list;
  if (!this->peek_token()->is_op(OPERATOR_EQ))
    {
      if (*last_expr_list == NULL)
	{
	  go_error_at(this->location(), "expected %<=%>");
	  return;
	}
      type = *last_type;
      expr_list = new Expression_list;
      for (Expression_list::const_iterator p = (*last_expr_list)->begin();
	   p != (*last_expr_list)->end();
	   ++p)
	{
	  Expression* copy = (*p)->copy();
	  copy->set_location(loc);
	  this->update_references(&copy);
	  expr_list->push_back(copy);
	}
    }
  else
    {
      this->advance_token();
      expr_list = this->expression_list(NULL, false, true);
      *last_type = type;
      if (*last_expr_list != NULL)
	delete *last_expr_list;
      *last_expr_list = expr_list;
    }

  Expression_list::const_iterator pe = expr_list->begin();
  for (Typed_identifier_list::iterator pi = til.begin();
       pi != til.end();
       ++pi, ++pe)
    {
      if (pe == expr_list->end())
	{
	  go_error_at(this->location(), "not enough initializers");
	  return;
	}
      if (type != NULL)
	pi->set_type(type);

      if (!Gogo::is_sink_name(pi->name()))
	this->gogo_->add_constant(*pi, *pe, iota);
      else
	{
	  static int count;
	  char buf[30];
	  snprintf(buf, sizeof buf, ".$sinkconst%d", count);
	  ++count;
	  Typed_identifier ti(std::string(buf), type, pi->location());
	  Named_object* no = this->gogo_->add_constant(ti, *pe, iota);
	  no->const_value()->set_is_sink();
	}
    }
  if (pe != expr_list->end())
    go_error_at(this->location(), "too many initializers");

  return;
}

// Update any references to names to refer to the current names,
// for weird cases like
//
// const X = 1
// func F() {
// 	const (
// 		X = X + X
//		Y
// 	)
// }
//
// where the X + X for the first X is the outer X, but the X + X
// copied for Y is the inner X.

class Update_references : public Traverse
{
 public:
  Update_references(Gogo* gogo)
    : Traverse(traverse_expressions),
      gogo_(gogo)
  { }

  int
  expression(Expression**);

 private:
  Gogo* gogo_;
};

int
Update_references::expression(Expression** pexpr)
{
  Named_object* old_no;
  switch ((*pexpr)->classification())
    {
    case Expression::EXPRESSION_CONST_REFERENCE:
      old_no = (*pexpr)->const_expression()->named_object();
      break;
    case Expression::EXPRESSION_VAR_REFERENCE:
      old_no = (*pexpr)->var_expression()->named_object();
      break;
    case Expression::EXPRESSION_ENCLOSED_VAR_REFERENCE:
      old_no = (*pexpr)->enclosed_var_expression()->variable();
      break;
    case Expression::EXPRESSION_FUNC_REFERENCE:
      old_no = (*pexpr)->func_expression()->named_object();
      break;
    case Expression::EXPRESSION_UNKNOWN_REFERENCE:
      old_no = (*pexpr)->unknown_expression()->named_object();
      break;
    default:
      return TRAVERSE_CONTINUE;
    }

  if (old_no->package() != NULL)
    {
      // This is a qualified reference, so it can't have changed in
      // scope.  FIXME: This probably doesn't handle dot imports
      // correctly.
      return TRAVERSE_CONTINUE;
    }

  Named_object* in_function;
  Named_object* new_no = this->gogo_->lookup(old_no->name(), &in_function);
  if (new_no == old_no)
    return TRAVERSE_CONTINUE;

  // The new name must be a constant, since that is all we have
  // introduced into scope.
  if (!new_no->is_const())
    {
      go_assert(saw_errors());
      return TRAVERSE_CONTINUE;
    }

  *pexpr = Expression::make_const_reference(new_no, (*pexpr)->location());

  return TRAVERSE_CONTINUE;
}

void
Parse::update_references(Expression** pexpr)
{
  Update_references ur(this->gogo_);
  ur.expression(pexpr);
  (*pexpr)->traverse_subexpressions(&ur);
}

// TypeDecl = "type" Decl<TypeSpec> .

void
Parse::type_decl()
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_TYPE));
  this->advance_token();
  this->decl(&Parse::type_spec);
}

// TypeSpec = identifier ["="] Type .

void
Parse::type_spec()
{
  unsigned int pragmas = this->lex_->get_and_clear_pragmas();
  this->check_directives();

  const Token* token = this->peek_token();
  if (!token->is_identifier())
    {
      go_error_at(this->location(), "expected identifier");
      return;
    }
  std::string name = token->identifier();
  bool is_exported = token->is_identifier_exported();
  Location location = token->location();
  token = this->advance_token();

  // Generics: a "[" after the type name introduces a type parameter
  // list, but only if it really is one -- "[" also begins an array or
  // slice type ("type S []int", "type A [3]int").  Capture the template
  // and return; instances are created on demand at each use site.
  // Normally skipped when re-parsing an instance (replay_tokens_ != NULL),
  // EXCEPT for a FUNCTION-LOCAL generic type declared inside a generic
  // function body being instantiated: that decl is at function scope (not
  // global) and must still be captured as its own generic-type template so
  // its own uses inside the body (e.g. "var x Box[int]") can instantiate it.
  // (A top-level generic type is only re-parsed as one of its own instances,
  // where the "[" is an array/slice of a concrete type, so global-scope
  // replay stays on the array path.)
  if (token->is_op(OPERATOR_LSQUARE)
      && (this->replay_tokens_ == NULL || !this->gogo_->in_global_scope())
      && this->next_is_type_parameter_decl())
    {
      this->generic_type_decl(name, is_exported, location);
      return;
    }

  bool is_alias = false;
  if (token->is_op(OPERATOR_EQ))
    {
      is_alias = true;
      token = this->advance_token();
    }

  // The scope of the type name starts at the point where the
  // identifier appears in the source code.  We implement this by
  // declaring the type before we read the type definition.
  Named_object* named_type = NULL;
  if (name != "_")
    {
      name = this->gogo_->pack_hidden_name(name, is_exported);
      named_type = this->gogo_->declare_type(name, location);
    }

  this->last_iface_terms_.clear();

  Type* type;
  if (name == "_" && token->is_keyword(KEYWORD_INTERFACE))
    {
      // We call Parse::interface_type explicity here because we do not want
      // to record an interface with a blank type name.
      type = this->interface_type(false);
    }
  else if (!token->is_op(OPERATOR_SEMICOLON))
    type = this->type();
  else
    {
      go_error_at(this->location(),
		  "unexpected semicolon or newline in type declaration");
      type = Type::make_error_type();
    }

  // Generics: if this type is a constraint interface with a type-set,
  // record its elements under its name so that uses of it as a type
  // parameter constraint can be enforced.
  if (name != "_" && !this->last_iface_terms_.empty())
    named_constraint_type_sets[name] = this->last_iface_terms_;
  this->last_iface_terms_.clear();

  if (type->is_error_type())
    {
      this->gogo_->mark_locals_used();
      while (!this->peek_token()->is_op(OPERATOR_SEMICOLON)
	     && !this->peek_token()->is_eof())
	this->advance_token();
    }

  if (name != "_")
    {
      if (named_type->is_type_declaration())
	{
	  Type* ftype = type->forwarded();
	  if (ftype->forward_declaration_type() != NULL
	      && (ftype->forward_declaration_type()->named_object()
		  == named_type))
	    {
	      go_error_at(location, "invalid recursive type");
	      type = Type::make_error_type();
	    }

	  Named_type* nt = Type::make_named_type(named_type, type, location);
	  if (is_alias)
	    nt->set_is_alias();

	  this->gogo_->define_type(named_type, nt);
	  go_assert(named_type->package() == NULL);

	  if ((pragmas & GOPRAGMA_NOTINHEAP) != 0)
	    {
	      nt->set_not_in_heap();
	      pragmas &= ~GOPRAGMA_NOTINHEAP;
	    }
	  if (pragmas != 0)
	    go_warning_at(location, 0,
			  "ignoring magic %<//go:...%> comment before type");
	}
      else
	{
	  // This will probably give a redefinition error.
	  this->gogo_->add_type(name, type, location);
	}
    }
}

// VarDecl = "var" Decl<VarSpec> .

void
Parse::var_decl()
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_VAR));
  this->advance_token();
  this->decl(&Parse::var_spec);
}

// VarSpec = IdentifierList
//             ( CompleteType [ "=" ExpressionList ] | "=" ExpressionList ) .

void
Parse::var_spec()
{
  Location loc = this->location();

  std::vector<std::string>* embeds = NULL;
  if (this->lex_->has_embeds())
    {
      if (!this->gogo_->current_file_imported_embed())
	go_error_at(loc, "invalid go:embed: missing import %<embed%>");
      else
	{
	  embeds = new(std::vector<std::string>);
	  this->lex_->get_and_clear_embeds(embeds);
	}
    }

  this->check_directives();

  // Get the variable names.
  Typed_identifier_list til;
  this->identifier_list(&til);

  if (embeds != NULL)
    {
      if (!this->gogo_->in_global_scope())
	{
	  go_error_at(loc, "go:embed only permitted at package scope");
	  embeds = NULL;
	}
      if (til.size() > 1)
	{
	  go_error_at(loc, "go:embed cannot apply to multiple vars");
	  embeds = NULL;
	}
    }

  Location location = this->location();

  Type* type = NULL;
  Expression_list* init = NULL;
  if (!this->peek_token()->is_op(OPERATOR_EQ))
    {
      type = this->type();
      if (type->is_error_type())
	{
	  this->gogo_->mark_locals_used();
	  while (!this->peek_token()->is_op(OPERATOR_EQ)
		 && !this->peek_token()->is_op(OPERATOR_SEMICOLON)
		 && !this->peek_token()->is_eof())
	    this->advance_token();
	}
      if (this->peek_token()->is_op(OPERATOR_EQ))
	{
	  this->advance_token();
	  init = this->expression_list(NULL, false, true);
	}
    }
  else
    {
      this->advance_token();
      init = this->expression_list(NULL, false, true);
    }

  if (embeds != NULL && init != NULL)
    {
      go_error_at(loc, "go:embed cannot apply to var with initializer");
      embeds = NULL;
    }

  this->init_vars(&til, type, init, false, embeds, location);

  if (init != NULL)
    delete init;
}

// Create variables.  TIL is a list of variable names.  If TYPE is not
// NULL, it is the type of all the variables.  If INIT is not NULL, it
// is an initializer list for the variables.

void
Parse::init_vars(const Typed_identifier_list* til, Type* type,
		 Expression_list* init, bool is_coloneq,
		 std::vector<std::string>* embeds, Location location)
{
  // Check for an initialization which can yield multiple values.
  if (init != NULL && init->size() == 1 && til->size() > 1)
    {
      go_assert(embeds == NULL);
      if (this->init_vars_from_call(til, type, *init->begin(), is_coloneq,
				    location))
	return;
      if (this->init_vars_from_map(til, type, *init->begin(), is_coloneq,
				   location))
	return;
      if (this->init_vars_from_receive(til, type, *init->begin(), is_coloneq,
				       location))
	return;
      if (this->init_vars_from_type_guard(til, type, *init->begin(),
					  is_coloneq, location))
	return;
    }

  if (init != NULL && init->size() != til->size())
    {
      if (init->empty() || !init->front()->is_error_expression())
	go_error_at(location, "wrong number of initializations");
      init = NULL;
      if (type == NULL)
	type = Type::make_error_type();
    }

  // Note that INIT was already parsed with the old name bindings, so
  // we don't have to worry that it will accidentally refer to the
  // newly declared variables.  But we do have to worry about a mix of
  // newly declared variables and old variables if the old variables
  // appear in the initializations.

  Expression_list::const_iterator pexpr;
  if (init != NULL)
    pexpr = init->begin();
  bool any_new = false;
  Expression_list* vars = new Expression_list();
  Expression_list* vals = new Expression_list();
  for (Typed_identifier_list::const_iterator p = til->begin();
       p != til->end();
       ++p)
    {
      if (init != NULL)
	go_assert(pexpr != init->end());
      Named_object* no = this->init_var(*p, type,
					init == NULL ? NULL : *pexpr,
					is_coloneq, false, &any_new,
					vars, vals);
      if (embeds != NULL && no->is_variable())
	no->var_value()->set_embeds(embeds);
      if (init != NULL)
	++pexpr;
    }
  if (init != NULL)
    go_assert(pexpr == init->end());
  if (is_coloneq && !any_new)
    go_error_at(location, "variables redeclared but no variable is new");
  this->finish_init_vars(vars, vals, location);
}

// See if we need to initialize a list of variables from a function
// call.  This returns true if we have set up the variables and the
// initialization.

bool
Parse::init_vars_from_call(const Typed_identifier_list* vars, Type* type,
			   Expression* expr, bool is_coloneq,
			   Location location)
{
  Call_expression* call = expr->call_expression();
  if (call == NULL)
    return false;

  // This is a function call.  We can't check here whether it returns
  // the right number of values, but it might.  Declare the variables,
  // and then assign the results of the call to them.

  call->set_expected_result_count(vars->size());

  Named_object* first_var = NULL;
  unsigned int index = 0;
  bool any_new = false;
  Expression_list* ivars = new Expression_list();
  Expression_list* ivals = new Expression_list();
  for (Typed_identifier_list::const_iterator pv = vars->begin();
       pv != vars->end();
       ++pv, ++index)
    {
      Expression* init = Expression::make_call_result(call, index);
      Named_object* no = this->init_var(*pv, type, init, is_coloneq, false,
					&any_new, ivars, ivals);

      if (this->gogo_->in_global_scope() && no->is_variable())
	{
	  if (first_var == NULL)
	    first_var = no;
	  else
	    {
              // If the current object is a redefinition of another object, we
              // might have already recorded the dependency relationship between
              // it and the first variable.  Either way, an error will be
              // reported for the redefinition and we don't need to properly
              // record dependency information for an invalid program.
              if (no->is_redefinition())
                continue;

	      // The subsequent vars have an implicit dependency on
	      // the first one, so that everything gets initialized in
	      // the right order and so that we detect cycles
	      // correctly.
	      this->gogo_->record_var_depends_on(no->var_value(), first_var);
	    }
	}
    }

  if (is_coloneq && !any_new)
    go_error_at(location, "variables redeclared but no variable is new");

  this->finish_init_vars(ivars, ivals, location);

  return true;
}

// See if we need to initialize a pair of values from a map index
// expression.  This returns true if we have set up the variables and
// the initialization.

bool
Parse::init_vars_from_map(const Typed_identifier_list* vars, Type* type,
			  Expression* expr, bool is_coloneq,
			  Location location)
{
  Index_expression* index = expr->index_expression();
  if (index == NULL)
    return false;
  if (vars->size() != 2)
    return false;

  // This is an index which is being assigned to two variables.  It
  // must be a map index.  Declare the variables, and then assign the
  // results of the map index.
  bool any_new = false;
  Typed_identifier_list::const_iterator p = vars->begin();
  Expression* init = type == NULL ? index : NULL;
  Named_object* val_no = this->init_var(*p, type, init, is_coloneq,
					type == NULL, &any_new, NULL, NULL);
  if (type == NULL && any_new && val_no->is_variable())
    val_no->var_value()->set_type_from_init_tuple();
  Expression* val_var = Expression::make_var_reference(val_no, location);

  ++p;
  Type* var_type = type;
  if (var_type == NULL)
    var_type = Type::lookup_bool_type();
  Named_object* no = this->init_var(*p, var_type, NULL, is_coloneq, false,
				    &any_new, NULL, NULL);
  Expression* present_var = Expression::make_var_reference(no, location);

  if (is_coloneq && !any_new)
    go_error_at(location, "variables redeclared but no variable is new");

  Statement* s = Statement::make_tuple_map_assignment(val_var, present_var,
						      index, location);

  if (!this->gogo_->in_global_scope())
    this->gogo_->add_statement(s);
  else if (!val_no->is_sink())
    {
      if (val_no->is_variable())
	{
	  val_no->var_value()->add_preinit_statement(this->gogo_, s);
	  if (no->is_variable())
	    this->gogo_->record_var_depends_on(no->var_value(), val_no);
	}
    }
  else if (!no->is_sink())
    {
      if (no->is_variable())
	no->var_value()->add_preinit_statement(this->gogo_, s);
    }
  else
    {
      // Execute the map index expression just so that we can fail if
      // the map is nil.
      Named_object* dummy = this->create_dummy_global(Type::lookup_bool_type(),
						      NULL, location);
      dummy->var_value()->add_preinit_statement(this->gogo_, s);
    }

  return true;
}

// See if we need to initialize a pair of values from a receive
// expression.  This returns true if we have set up the variables and
// the initialization.

bool
Parse::init_vars_from_receive(const Typed_identifier_list* vars, Type* type,
			      Expression* expr, bool is_coloneq,
			      Location location)
{
  Receive_expression* receive = expr->receive_expression();
  if (receive == NULL)
    return false;
  if (vars->size() != 2)
    return false;

  // This is a receive expression which is being assigned to two
  // variables.  Declare the variables, and then assign the results of
  // the receive.
  bool any_new = false;
  Typed_identifier_list::const_iterator p = vars->begin();
  Expression* init = type == NULL ? receive : NULL;
  Named_object* val_no = this->init_var(*p, type, init, is_coloneq,
					type == NULL, &any_new, NULL, NULL);
  if (type == NULL && any_new && val_no->is_variable())
    val_no->var_value()->set_type_from_init_tuple();
  Expression* val_var = Expression::make_var_reference(val_no, location);

  ++p;
  Type* var_type = type;
  if (var_type == NULL)
    var_type = Type::lookup_bool_type();
  Named_object* no = this->init_var(*p, var_type, NULL, is_coloneq, false,
				    &any_new, NULL, NULL);
  Expression* received_var = Expression::make_var_reference(no, location);

  if (is_coloneq && !any_new)
    go_error_at(location, "variables redeclared but no variable is new");

  Statement* s = Statement::make_tuple_receive_assignment(val_var,
							  received_var,
							  receive->channel(),
							  location);

  if (!this->gogo_->in_global_scope())
    this->gogo_->add_statement(s);
  else if (!val_no->is_sink())
    {
      if (val_no->is_variable())
	{
	  val_no->var_value()->add_preinit_statement(this->gogo_, s);
	  if (no->is_variable())
	    this->gogo_->record_var_depends_on(no->var_value(), val_no);
	}
    }
  else if (!no->is_sink())
    {
      if (no->is_variable())
	no->var_value()->add_preinit_statement(this->gogo_, s);
    }
  else
    {
      Named_object* dummy = this->create_dummy_global(Type::lookup_bool_type(),
						      NULL, location);
      dummy->var_value()->add_preinit_statement(this->gogo_, s);
    }

  return true;
}

// See if we need to initialize a pair of values from a type guard
// expression.  This returns true if we have set up the variables and
// the initialization.

bool
Parse::init_vars_from_type_guard(const Typed_identifier_list* vars,
				 Type* type, Expression* expr,
				 bool is_coloneq, Location location)
{
  Type_guard_expression* type_guard = expr->type_guard_expression();
  if (type_guard == NULL)
    return false;
  if (vars->size() != 2)
    return false;

  // This is a type guard expression which is being assigned to two
  // variables.  Declare the variables, and then assign the results of
  // the type guard.
  bool any_new = false;
  Typed_identifier_list::const_iterator p = vars->begin();
  Type* var_type = type;
  if (var_type == NULL)
    var_type = type_guard->type();
  Named_object* val_no = this->init_var(*p, var_type, NULL, is_coloneq, false,
					&any_new, NULL, NULL);
  Expression* val_var = Expression::make_var_reference(val_no, location);

  ++p;
  var_type = type;
  if (var_type == NULL)
    var_type = Type::lookup_bool_type();
  Named_object* no = this->init_var(*p, var_type, NULL, is_coloneq, false,
				    &any_new, NULL, NULL);
  Expression* ok_var = Expression::make_var_reference(no, location);

  Expression* texpr = type_guard->expr();
  Type* t = type_guard->type();
  Statement* s = Statement::make_tuple_type_guard_assignment(val_var, ok_var,
							     texpr, t,
							     location);

  if (is_coloneq && !any_new)
    go_error_at(location, "variables redeclared but no variable is new");

  if (!this->gogo_->in_global_scope())
    this->gogo_->add_statement(s);
  else if (!val_no->is_sink())
    {
      if (val_no->is_variable())
	{
	  val_no->var_value()->add_preinit_statement(this->gogo_, s);
	  if (no->is_variable())
	    this->gogo_->record_var_depends_on(no->var_value(), val_no);
	}
    }
  else if (!no->is_sink())
    {
      if (no->is_variable())
	no->var_value()->add_preinit_statement(this->gogo_, s);
    }
  else
    {
      Named_object* dummy = this->create_dummy_global(type, NULL, location);
      dummy->var_value()->add_preinit_statement(this->gogo_, s);
    }

  return true;
}

// Create a single variable.  If IS_COLONEQ is true, we permit
// redeclarations in the same block, and we set *IS_NEW when we find a
// new variable which is not a redeclaration.

Named_object*
Parse::init_var(const Typed_identifier& tid, Type* type, Expression* init,
		bool is_coloneq, bool type_from_init, bool* is_new,
		Expression_list* vars, Expression_list* vals)
{
  Location location = tid.location();

  if (Gogo::is_sink_name(tid.name()))
    {
      if (!type_from_init && init != NULL)
	{
	  if (this->gogo_->in_global_scope())
	    return this->create_dummy_global(type, init, location);
	  else
	    {
	      // Create a dummy variable so that we will check whether the
	      // initializer can be assigned to the type.
	      Variable* var = new Variable(type, init, false, false, false,
					   location);
	      var->set_is_used();
	      static int count;
	      char buf[30];
	      snprintf(buf, sizeof buf, "sink$%d", count);
	      ++count;
	      return this->gogo_->add_variable(buf, var);
	    }
	}
      if (type != NULL)
	this->gogo_->add_type_to_verify(type);
      return this->gogo_->add_sink();
    }

  if (is_coloneq)
    {
      Named_object* no = this->gogo_->lookup_in_block(tid.name());
      if (no != NULL
	  && (no->is_variable() || no->is_result_variable()))
	{
	  // INIT may be NULL even when IS_COLONEQ is true for cases
	  // like v, ok := x.(int).
	  if (!type_from_init && init != NULL)
	    {
	      go_assert(vars != NULL && vals != NULL);
	      vars->push_back(Expression::make_var_reference(no, location));
	      vals->push_back(init);
	    }
	  return no;
	}
    }
  *is_new = true;
  Variable* var = new Variable(type, init, this->gogo_->in_global_scope(),
			       false, false, location);
  Named_object* no = this->gogo_->add_variable(tid.name(), var);
  if (!no->is_variable())
    {
      // The name is already defined, so we just gave an error.
      return this->gogo_->add_sink();
    }
  return no;
}

// Create a dummy global variable to force an initializer to be run in
// the right place.  This is used when a sink variable is initialized
// at global scope.

Named_object*
Parse::create_dummy_global(Type* type, Expression* init,
			   Location location)
{
  if (type == NULL && init == NULL)
    type = Type::lookup_bool_type();
  Variable* var = new Variable(type, init, true, false, false, location);
  var->set_is_global_sink();
  static int count;
  char buf[30];
  snprintf(buf, sizeof buf, "_.%d", count);
  ++count;
  return this->gogo_->add_variable(buf, var);
}

// Finish the variable initialization by executing any assignments to
// existing variables when using :=.  These must be done as a tuple
// assignment in case of something like n, a, b := 1, b, a.

void
Parse::finish_init_vars(Expression_list* vars, Expression_list* vals,
			Location location)
{
  if (vars->empty())
    {
      delete vars;
      delete vals;
    }
  else if (vars->size() == 1)
    {
      go_assert(!this->gogo_->in_global_scope());
      this->gogo_->add_statement(Statement::make_assignment(vars->front(),
							    vals->front(),
							    location));
      delete vars;
      delete vals;
    }
  else
    {
      go_assert(!this->gogo_->in_global_scope());
      this->gogo_->add_statement(Statement::make_tuple_assignment(vars, vals,
								  location));
    }
}

// SimpleVarDecl = identifier ":=" Expression .

// We've already seen the identifier.

// FIXME: We also have to implement
//  IdentifierList ":=" ExpressionList
// In order to support both "a, b := 1, 0" and "a, b = 1, 0" we accept
// tuple assignments here as well.

// If MAY_BE_COMPOSITE_LIT is true, the expression on the right hand
// side may be a composite literal.

// If P_RANGE_CLAUSE is not NULL, then this will recognize a
// RangeClause.

// If P_TYPE_SWITCH is not NULL, this will recognize a type switch
// guard (var := expr.("type") using the literal keyword "type").

void
Parse::simple_var_decl_or_assignment(const std::string& name,
				     Location location,
				     bool may_be_composite_lit,
				     Range_clause* p_range_clause,
				     Type_switch* p_type_switch)
{
  Typed_identifier_list til;
  til.push_back(Typed_identifier(name, NULL, location));

  std::set<std::string> uniq_idents;
  uniq_idents.insert(name);
  std::string dup_name;
  Location dup_loc;

  // We've seen one identifier.  If we see a comma now, this could be
  // "a, *p = 1, 2".
  if (this->peek_token()->is_op(OPERATOR_COMMA))
    {
      go_assert(p_type_switch == NULL);
      while (true)
	{
	  const Token* token = this->advance_token();
	  if (!token->is_identifier())
	    break;

	  std::string id = token->identifier();
	  bool is_id_exported = token->is_identifier_exported();
	  Location id_location = token->location();
	  std::pair<std::set<std::string>::iterator, bool> ins;

	  token = this->advance_token();
	  if (!token->is_op(OPERATOR_COMMA))
	    {
	      if (token->is_op(OPERATOR_COLONEQ))
		{
		  id = this->gogo_->pack_hidden_name(id, is_id_exported);
		  ins = uniq_idents.insert(id);
		  if (!ins.second && !Gogo::is_sink_name(id))
		    {
		      // Use %s to print := to avoid -Wformat-diag warning.
		      go_error_at(id_location,
				  "%qs repeated on left side of %s",
				  Gogo::message_name(id).c_str(), ":=");
		      id = this->gogo_->pack_hidden_name("_", false);
		    }
		  til.push_back(Typed_identifier(id, NULL, location));
		}
	      else
		this->unget_token(Token::make_identifier_token(id,
							       is_id_exported,
							       id_location));
	      break;
	    }

	  id = this->gogo_->pack_hidden_name(id, is_id_exported);
	  ins = uniq_idents.insert(id);
	  std::string name = id;
	  if (!ins.second && !Gogo::is_sink_name(id))
	    {
	      dup_name = Gogo::message_name(id);
	      dup_loc = id_location;
	      id = this->gogo_->pack_hidden_name("_", false);
	    }
	  til.push_back(Typed_identifier(id, NULL, location));
	}

      // We have a comma separated list of identifiers in TIL.  If the
      // next token is COLONEQ, then this is a simple var decl, and we
      // have the complete list of identifiers.  If the next token is
      // not COLONEQ, then the only valid parse is a tuple assignment.
      // The list of identifiers we have so far is really a list of
      // expressions.  There are more expressions following.

      if (!this->peek_token()->is_op(OPERATOR_COLONEQ))
	{
	  Expression_list* exprs = new Expression_list;
	  for (Typed_identifier_list::const_iterator p = til.begin();
	       p != til.end();
	       ++p)
	    exprs->push_back(this->id_to_expression(p->name(), p->location(),
						    true, false));

	  Expression_list* more_exprs =
	    this->expression_list(NULL, true, may_be_composite_lit);
	  for (Expression_list::const_iterator p = more_exprs->begin();
	       p != more_exprs->end();
	       ++p)
	    exprs->push_back(*p);
	  delete more_exprs;

	  this->tuple_assignment(exprs, may_be_composite_lit, p_range_clause);
	  return;
	}
    }

  go_assert(this->peek_token()->is_op(OPERATOR_COLONEQ));
  const Token* token = this->advance_token();

  if (!dup_name.empty())
    {
      // Use %s to print := to avoid -Wformat-diag warning.
      go_error_at(dup_loc, "%qs repeated on left side of %s",
		  dup_name.c_str(), ":=");
    }

  if (p_range_clause != NULL && token->is_keyword(KEYWORD_RANGE))
    {
      this->range_clause_decl(&til, p_range_clause);
      return;
    }

  Expression_list* init;
  if (p_type_switch == NULL)
    init = this->expression_list(NULL, false, may_be_composite_lit);
  else
    {
      bool is_type_switch = false;
      Expression* expr = this->expression(PRECEDENCE_NORMAL, false,
					  may_be_composite_lit,
					  &is_type_switch, NULL);
      if (is_type_switch)
	{
	  p_type_switch->found = true;
	  p_type_switch->name = name;
	  p_type_switch->location = location;
	  p_type_switch->expr = expr;
	  return;
	}

      if (!this->peek_token()->is_op(OPERATOR_COMMA))
	{
	  init = new Expression_list();
	  init->push_back(expr);
	}
      else
	{
	  this->advance_token();
	  init = this->expression_list(expr, false, may_be_composite_lit);
	}
    }

  this->init_vars(&til, NULL, init, true, NULL, location);
}

// FunctionDecl = "func" identifier Signature [ Block ] .
// MethodDecl = "func" Receiver identifier Signature [ Block ] .

// Deprecated gcc extension:
//   FunctionDecl = "func" identifier Signature
//                    __asm__ "(" string_lit ")" .
// This extension means a function whose real name is the identifier
// inside the asm.  This extension will be removed at some future
// date.  It has been replaced with //extern or //go:linkname comments.
//
// PRAGMAS is a bitset of magic comments.

void
Parse::function_decl()
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_FUNC));

  unsigned int pragmas = this->lex_->get_and_clear_pragmas();
  this->check_directives();

  Location location = this->location();
  std::string extern_name = this->lex_->extern_name();
  const Token* token = this->advance_token();

  bool expected_receiver = false;
  Typed_identifier* rec = NULL;
  if (token->is_op(OPERATOR_LPAREN))
    {
      expected_receiver = true;
      // Generics: when not re-parsing an instance, detect a generic
      // receiver (one containing a "[" type parameter list) by first
      // capturing the receiver group.
      if (this->replay_tokens_ == NULL)
	{
	  std::vector<Token> recv;
	  this->capture_bracket_group(&recv);
	  bool is_generic_recv = false;
	  for (size_t i = 0; i < recv.size(); ++i)
	    if (recv[i].is_op(OPERATOR_LSQUARE))
	      {
		is_generic_recv = true;
		break;
	      }
	  if (is_generic_recv)
	    {
	      this->generic_method_decl(recv, location, pragmas);
	      return;
	    }
	  // Ordinary method: parse the receiver from the captured tokens.
	  recv.push_back(Token::make_eof_token(location));
	  Parse rp(this->lex_, this->gogo_);
	  rp.set_replay_tokens(&recv);
	  rec = rp.receiver();
	  token = this->peek_token();
	}
      else
	{
	  rec = this->receiver();
	  token = this->peek_token();
	}
    }

  if (!token->is_identifier())
    {
      go_error_at(this->location(), "expected function name");
      return;
    }

  bool is_exported = token->is_identifier_exported();
  std::string name =
    this->gogo_->pack_hidden_name(token->identifier(), is_exported);

  this->advance_token();

  // Generics: a "[" after the function name introduces a type
  // parameter list.  We capture the template and return; instances are
  // created on demand at each use site.  (Skipped when re-parsing an
  // instance, where the brackets hold concrete type arguments.)
  if (this->peek_token()->is_op(OPERATOR_LSQUARE) && rec == NULL
      && this->replay_tokens_ == NULL)
    {
      this->generic_function_decl(name, is_exported, location, pragmas);
      return;
    }

  Function_type* fntype = this->signature(rec, this->location());

  Named_object* named_object = NULL;

  if (this->peek_token()->is_keyword(KEYWORD_ASM))
    {
      if (!this->advance_token()->is_op(OPERATOR_LPAREN))
	{
	  go_error_at(this->location(), "expected %<(%>");
	  return;
	}
      token = this->advance_token();
      if (!token->is_string())
	{
	  go_error_at(this->location(), "expected string");
	  return;
	}
      std::string asm_name = token->string_value();
      if (!this->advance_token()->is_op(OPERATOR_RPAREN))
	{
	  go_error_at(this->location(), "expected %<)%>");
	  return;
	}
      this->advance_token();
      if (!Gogo::is_sink_name(name))
	{
	  named_object = this->gogo_->declare_function(name, fntype, location);
	  if (named_object->is_function_declaration())
	    named_object->func_declaration_value()->set_asm_name(asm_name);
	}
    }

  // Check for the easy error of a newline before the opening brace.
  if (this->peek_token()->is_op(OPERATOR_SEMICOLON))
    {
      Location semi_loc = this->location();
      if (this->advance_token()->is_op(OPERATOR_LCURLY))
	go_error_at(this->location(),
		    "unexpected semicolon or newline before %<{%>");
      else
	this->unget_token(Token::make_operator_token(OPERATOR_SEMICOLON,
						     semi_loc));
    }

  static struct {
    unsigned int bit;
    const char* name;
    bool decl_ok;
    bool func_ok;
    bool method_ok;
  } pragma_check[] =
      {
	{ GOPRAGMA_NOINTERFACE, "nointerface", false, false, true },
	{ GOPRAGMA_NOESCAPE, "noescape", true, false, false },
	{ GOPRAGMA_NORACE, "norace", false, true, true },
	{ GOPRAGMA_NOSPLIT, "nosplit", false, true, true },
	{ GOPRAGMA_NOINLINE, "noinline", false, true, true },
	{ GOPRAGMA_SYSTEMSTACK, "systemstack", false, true, true },
	{ GOPRAGMA_NOWRITEBARRIER, "nowritebarrier", false, true, true },
	{ GOPRAGMA_NOWRITEBARRIERREC, "nowritebarrierrec", false, true,
	  true },
	{ GOPRAGMA_YESWRITEBARRIERREC, "yeswritebarrierrec", false, true,
	  true },
	{ GOPRAGMA_CGOUNSAFEARGS, "cgo_unsafe_args", false, true, true },
	{ GOPRAGMA_UINTPTRESCAPES, "uintptrescapes", true, true, true },
      };

  bool is_decl = !this->peek_token()->is_op(OPERATOR_LCURLY);
  if (pragmas != 0)
    {
      for (size_t i = 0;
	   i < sizeof(pragma_check) / sizeof(pragma_check[0]);
	   ++i)
	{
	  if ((pragmas & pragma_check[i].bit) == 0)
	    continue;

	  if (is_decl)
	    {
	      if (pragma_check[i].decl_ok)
		continue;
	      go_warning_at(location, 0,
			    ("ignoring magic %<//go:%s%> comment "
			     "before declaration"),
			    pragma_check[i].name);
	    }
	  else if (rec == NULL)
	    {
	      if (pragma_check[i].func_ok)
		continue;
	      go_warning_at(location, 0,
			    ("ignoring magic %<//go:%s%> comment "
			     "before function definition"),
			    pragma_check[i].name);
	    }
	  else
	    {
	      if (pragma_check[i].method_ok)
		continue;
	      go_warning_at(location, 0,
			    ("ignoring magic %<//go:%s%> comment "
			     "before method definition"),
			    pragma_check[i].name);
	    }

	  pragmas &= ~ pragma_check[i].bit;
	}
    }

  if (is_decl)
    {
      if (named_object == NULL)
	{
          // Function declarations with the blank identifier as a name are
          // mostly ignored since they cannot be called.  We make an object
          // for this declaration for type-checking purposes.
          if (Gogo::is_sink_name(name))
            {
              static int count;
              char buf[30];
              snprintf(buf, sizeof buf, ".$sinkfndecl%d", count);
              ++count;
              name = std::string(buf);
            }

	  if (fntype == NULL
              || (expected_receiver && rec == NULL))
	    this->gogo_->add_erroneous_name(name);
	  else
	    {
	      named_object = this->gogo_->declare_function(name, fntype,
							   location);
	      if (!extern_name.empty()
		  && named_object->is_function_declaration())
		{
		  Function_declaration* fd =
		    named_object->func_declaration_value();
		  fd->set_asm_name(extern_name);
		}
	    }
	}

      if (pragmas != 0 && named_object->is_function_declaration())
	named_object->func_declaration_value()->set_pragmas(pragmas);
    }
  else
    {
      bool hold_is_erroneous_function = this->is_erroneous_function_;
      if (fntype == NULL)
	{
	  fntype = Type::make_function_type(NULL, NULL, NULL, location);
	  this->is_erroneous_function_ = true;
	  if (!Gogo::is_sink_name(name))
	    this->gogo_->add_erroneous_name(name);
	  name = this->gogo_->pack_hidden_name("_", false);
	}
      named_object = this->gogo_->start_function(name, fntype, true, location);
      Location end_loc = this->block();
      this->gogo_->finish_function(end_loc);

      if (pragmas != 0
	  && !this->is_erroneous_function_
	  && named_object->is_function())
	named_object->func_value()->set_pragmas(pragmas);
      this->is_erroneous_function_ = hold_is_erroneous_function;
    }
}

// Build a stable string key for a token, used to identify a particular
// set of type arguments for generic instantiation caching.

static std::string
token_key_string(const Token& t)
{
  char buf[64];
  switch (t.classification())
    {
    case Token::TOKEN_IDENTIFIER:
      return t.identifier();
    case Token::TOKEN_KEYWORD:
      snprintf(buf, sizeof buf, "k%d", (int) t.keyword());
      return std::string(buf);
    case Token::TOKEN_OPERATOR:
      snprintf(buf, sizeof buf, "o%d", (int) t.op());
      return std::string(buf);
    case Token::TOKEN_INTEGER:
      {
	char* s = mpz_get_str(NULL, 16, *t.integer_value());
	std::string ret = std::string("i") + s;
	free(s);
	return ret;
      }
    case Token::TOKEN_STRING:
      return std::string("s") + t.string_value();
    default:
      return "?";
    }
}

// Generics: the canonical spelling (as a token sequence) of each generic
// type instance, e.g. the instance named "Box$type0" maps to the tokens
// "Box [ int ]".  When type inference needs to express an already-created
// instance as a type argument, emitting this spelling (rather than the
// generated instance name) makes the instance-cache key match the one
// produced when the same type is written out directly, so a single
// instance is shared.  Keyed by the instance's Named_object; valid for
// the lifetime of the (single-package) compilation.
static std::map<const Named_object*, std::vector<Token> >
  generic_instance_spelling;

// Generics: the explicit type arguments of a partial instantiation of a
// generic function, e.g. "F[int]" where F has more than one type
// parameter and the rest are to be inferred from the call arguments.
// Keyed by the function-reference expression so the call expression can
// recover them and seed inference.
static std::map<const Expression*, std::vector<std::vector<Token> > >
  partial_generic_type_args;

const std::vector<std::vector<Token> >*
Parse::partial_type_args_for(const Expression* expr)
{
  std::map<const Expression*, std::vector<std::vector<Token> > >::const_iterator
    p = partial_generic_type_args.find(expr);
  if (p == partial_generic_type_args.end())
    return NULL;
  return &p->second;
}

// Generics: the package-qualifier alias->pkgpath bindings for a partial
// instantiation's explicit type arguments, captured at parse time (while the
// file's imports are still in scope).  The explicit arguments are re-parsed
// later, during determine_types, when the file-scope imports have been
// cleared; without these bindings a qualifier like "tpm2" in an explicit
// argument "tpm2.TPMTPublic" would not resolve.  Keyed like
// partial_generic_type_args.
static std::map<const Expression*, std::map<std::string, std::string> >
  partial_generic_type_arg_aliases;

const std::map<std::string, std::string>*
Parse::partial_type_arg_aliases_for(const Expression* expr)
{
  std::map<const Expression*,
	   std::map<std::string, std::string> >::const_iterator
    p = partial_generic_type_arg_aliases.find(expr);
  if (p == partial_generic_type_arg_aliases.end())
    return NULL;
  return &p->second;
}

// Generics: for an ambiguous "name[expr](args)" on a forward (unknown)
// reference, "[expr]" may be a generic type-argument list (a generic call
// "F[T](args)") or an ordinary index of a value followed by a call
// ("(arr[i])(args)").  The parser cannot tell until the name resolves, so it
// records the call as a generic instantiation (partial_generic_type_args
// above) but also stores the ordinary index interpretation here as a
// fallback.  If the name turns out to be a non-generic value,
// Call_expression::do_determine_type swaps in this fallback.  Keyed by the
// (kept) function-reference expression.
static std::map<const Expression*, Expression*> partial_generic_call_fallback;

Expression*
Parse::partial_call_fallback_for(const Expression* expr)
{
  std::map<const Expression*, Expression*>::const_iterator
    p = partial_generic_call_fallback.find(expr);
  if (p == partial_generic_call_fallback.end())
    return NULL;
  return p->second;
}

// Generics: a forward reference "F[args]" used as a value (no call, no
// composite literal) is ambiguous until F is resolved: it is either an
// instantiation of a generic function or an index of a value.  These maps
// record, keyed by the unknown reference expression, the captured type
// arguments and the fallback index expression; Parse::resolve_generic_value
// picks between them once the name resolves.
static std::map<const Expression*, std::vector<std::vector<Token> > >
  generic_value_type_args;
static std::map<const Expression*, Expression*> generic_value_fallback;

// Generics: resolve such a deferred "F[args]" value.  RESOLVED_NO is the
// named object the reference resolved to.  Returns the instantiated
// function reference if RESOLVED_NO is a generic function, the fallback
// index expression otherwise, or NULL if KEY was not a deferred value.

// Generics: whether the captured bracket content GROUP is unambiguously a
// type (so that "F[group]" on a forward reference may be a generic-function
// instantiation), as opposed to an index expression.  Conservative: only
// returns true for forms that cannot be a value -- a composite type
// ("[]E", "*E", "map[..]", "chan E", "func(..)", "struct{..}",
// "interface{..}", "~E"), a parenthesized type, or a single identifier that
// already names a type (predeclared or package-level).  Anything else
// (numbers, selectors, arithmetic, names that are not types) is treated as
// an index, preserving ordinary indexing including the comma-ok map form.

// Generics: whether NAME (with exportedness IS_EXPORTED) names a registered
// generic type.  Checks the current package's packing and, while re-parsing
// an imported template, the template's defining package (whose pkgpath the
// name would not otherwise be packed with).  Used to recognize a generic-type
// instantiation "Foo[...]" as an unnamed parameter rather than a named
// parameter of array type.

bool
Parse::name_is_generic_type(const std::string& name, bool is_exported)
{
  if (this->gogo_->lookup_generic_type(
	this->gogo_->pack_hidden_name(name, is_exported)) != NULL)
    return true;
  Package* ip = this->gogo_->current_instantiation_package();
  if (ip != NULL
      && this->gogo_->lookup_generic_type(
	   ip->pkgpath() + '.' + Gogo::unpack_hidden_name(name)) != NULL)
    return true;
  return false;
}

bool
Parse::group_is_clearly_type(const std::vector<Token>& group)
{
  if (group.empty())
    return false;
  const Token& t0 = group[0];
  // A leading "*" is ambiguous: "*E" is a pointer type, but "*p" is a
  // pointer dereference (so "v[*p]" is an ordinary index, including the
  // comma-ok map form).  Decide by what follows the "*": treat it as a type
  // only if the remainder is itself clearly a type (e.g. "*int", "*[]E"),
  // not when it is a value such as a variable ("*p") or expression.
  if (t0.is_op(OPERATOR_MULT))
    {
      std::vector<Token> tail(group.begin() + 1, group.end());
      return this->group_is_clearly_type(tail);
    }
  if (t0.is_op(OPERATOR_LSQUARE)
      || t0.is_op(OPERATOR_CHANOP) || t0.is_op(OPERATOR_TILDE)
      || t0.is_keyword(KEYWORD_CHAN) || t0.is_keyword(KEYWORD_MAP)
      || t0.is_keyword(KEYWORD_FUNC) || t0.is_keyword(KEYWORD_STRUCT)
      || t0.is_keyword(KEYWORD_INTERFACE))
    return true;
  if (group.size() == 1 && t0.is_identifier())
    {
      // A bare name: a type only if it already resolves to one (no side
      // effects -- do not create an unknown name).
      const std::string& nm = t0.identifier();
      Named_object* no = this->gogo_->lookup_global(nm.c_str());
      if (no == NULL)
	no = this->gogo_->lookup(
	  this->gogo_->pack_hidden_name(nm, Lex::is_exported_name(nm)), NULL);
      return no != NULL && (no->is_type() || no->is_type_declaration());
    }
  return false;
}

Expression*
Parse::resolve_generic_value(Gogo* gogo, const Expression* key,
			     Named_object* resolved_no, Location location)
{
  std::map<const Expression*, std::vector<std::vector<Token> > >::iterator p =
    generic_value_type_args.find(key);
  if (p == generic_value_type_args.end())
    return NULL;
  std::vector<std::vector<Token> > type_args = p->second;
  Expression* fallback = generic_value_fallback[key];

  Generic_function_info* gi = (resolved_no == NULL
			       ? NULL
			       : gogo->lookup_generic_function_no(resolved_no));
  if (gi == NULL)
    return fallback;

  // The parser only queries pragmas/embeds from the lexer (empty here), so
  // a dummy lexer is fine, as in the generic-call path.
  Lex dummy_lex(NULL, NULL, gogo->linemap());
  Parse parse(&dummy_lex, gogo);
  Named_object* inst =
    parse.instantiate_generic_function(gi, type_args, location);
  if (inst == NULL)
    return Expression::make_error(location);
  return Expression::make_func_reference(inst, NULL, location);
}

// Forward declarations; defined below.
static void
substitute_type_params(const std::vector<Token>&,
		       const std::vector<std::string>&,
		       const std::vector<std::vector<Token> >&,
		       std::vector<Token>&);
static std::vector<std::vector<Token> >
split_top_level(const std::vector<Token>&, Operator);
static void
record_constraint_obligations(Gogo*, Generic_function_info*,
			      const std::vector<std::vector<Token> >&,
			      const std::string&, Location,
			      const std::map<std::string, std::string>*
			        extra_pkg_aliases = NULL);
static bool
type_to_tokens(Type*, std::vector<Token>&, Location,
	       std::map<std::string, std::string>* pkg_bindings = NULL);

// Generics: parse a "[name constraint, ...]" type parameter list,
// collecting the parameter names.  The current token is "[".  Grouped
// parameters ("[T, U any]") work because every comma-separated
// identifier is collected as a name; constraints are parsed but
// otherwise ignored (no constraint checking yet).

void
Parse::type_parameter_names(std::vector<std::string>* names,
			    std::vector<std::vector<Token> >* constraints)
{
  go_assert(this->peek_token()->is_op(OPERATOR_LSQUARE));
  this->advance_token();
  while (!this->peek_token()->is_op(OPERATOR_RSQUARE)
	 && !this->peek_token()->is_eof())
    {
      const Token* token = this->peek_token();
      if (!token->is_identifier())
	{
	  go_error_at(this->location(), "expected type parameter name");
	  break;
	}
      // A blank type-parameter name "_" is not referenceable in the body,
      // so give it a unique synthetic name.  Otherwise substitution would
      // rewrite every blank identifier in the body (e.g. "_ = expr").
      if (token->identifier() == "_")
	{
	  static unsigned int blank_count;
	  char buf[32];
	  snprintf(buf, sizeof buf, "$blanktparam%u", blank_count);
	  ++blank_count;
	  names->push_back(std::string(buf));
	}
      else
	names->push_back(token->identifier());
      this->advance_token();

      // Capture the constraint tokens up to a top-level "," or "]".  In a
      // group "[A, B, C cons]" only the last name is followed by the
      // constraint; the earlier ones get an empty one (so their constraint
      // is not separately enforced, which never rejects valid code).
      std::vector<Token> constraint;
      int cdepth = 0;
      while (true)
	{
	  const Token* t = this->peek_token();
	  if (t->is_eof())
	    break;
	  if (cdepth == 0
	      && (t->is_op(OPERATOR_COMMA) || t->is_op(OPERATOR_RSQUARE)))
	    break;
	  if (t->is_op(OPERATOR_LSQUARE) || t->is_op(OPERATOR_LPAREN)
	      || t->is_op(OPERATOR_LCURLY))
	    ++cdepth;
	  else if (t->is_op(OPERATOR_RSQUARE) || t->is_op(OPERATOR_RPAREN)
		   || t->is_op(OPERATOR_RCURLY))
	    --cdepth;
	  constraint.push_back(*t);
	  this->advance_token();
	}
      if (constraints != NULL)
	constraints->push_back(constraint);
      if (this->peek_token()->is_op(OPERATOR_COMMA))
	this->advance_token();
    }
  // Consume "]".
  this->advance_token();
}

// Generics: the current token is the "[" that follows a type name in a
// type declaration.  Return whether it introduces a type parameter list
// ("[P constraint, ...]") rather than an array or slice element type
// ("[]T", "[N]T", "[N/64]T", ...).  Looks ahead two tokens and restores
// the stream.
//
// A type parameter list has the form "[Name Constraint, ...]": the first
// identifier is a parameter name, immediately followed by a constraint (a
// type) or, for multiple parameters, a comma.  An array type instead has
// the form "[Expr]Elem", where Expr is a constant expression; if it begins
// with an identifier, the next token continues that expression (a binary
// operator, a selector ".", an index, etc.).
//
// So after "[" Name, it is a type parameter list only if the following
// token can begin a constraint or is a comma.  A constraint (an interface,
// or a type used as a one-element type set) begins with another identifier,
// "~", "[" (e.g. "[]byte"), "<-", or one of the type keywords
// (interface/chan/func/map/struct).  Anything else -- "]", a binary
// operator such as "/", "*", "-", "|", a ".", etc. -- means "[" began an
// array type.  This matches the gc compiler; note in particular that
// "[P *int]" is the array "P * int", not a pointer constraint.
//
// The distinguishing cases:
//   "[" "]"                     -> slice type
//   "[" non-ident ...           -> array type ("[3]T", "[2*N]T")
//   "[" ident "]"               -> array type "[N]T"
//   "[" ident <binop|.|(> ...   -> array type "[N/64]T", "[N*M]T", ...
//   "[" ident ","               -> type parameter list
//   "[" ident <ident|~|[|<-|kw> -> type parameter list "[P constraint]"

bool
Parse::next_is_type_parameter_decl()
{
  // Scan the whole bracket ("[" ... matching "]") into a buffer so we can
  // look past the first two tokens, then restore the token stream.  We need
  // more than a two-token lookahead for the ambiguous "[Name *..." case: gc
  // parses "[P *T]" as the array "P * T", but "[T *X | Y]" (a union constraint
  // with a pointer term) and "[T *X, U any]" (multiple parameters) are type
  // parameter lists, disambiguated by a top-level "|" or ",".  (net/x509's
  // "nameConstraintsSet[T *net.IPNet | string, V net.IP | string]" hits this.)
  std::vector<Token> saved;
  saved.push_back(*this->peek_token());   // the "[" (current token)
  int depth = 0;
  bool has_toplevel_pipe = false;
  bool has_toplevel_comma = false;
  Token t1 = Token::make_invalid_token(Linemap::unknown_location());
  Token t2 = Token::make_invalid_token(Linemap::unknown_location());
  size_t meaningful = 0;
  while (true)
    {
      const Token* t = this->advance_token();
      saved.push_back(*t);
      if (t->is_eof())
	break;
      if (depth == 0 && t->is_op(OPERATOR_RSQUARE))
	break;
      if (t->is_op(OPERATOR_LSQUARE) || t->is_op(OPERATOR_LPAREN)
	  || t->is_op(OPERATOR_LCURLY))
	++depth;
      else if (t->is_op(OPERATOR_RPAREN) || t->is_op(OPERATOR_RCURLY)
	       || t->is_op(OPERATOR_RSQUARE))
	--depth;
      else if (depth == 0 && t->is_op(OPERATOR_OR))
	has_toplevel_pipe = true;
      else if (depth == 0 && t->is_op(OPERATOR_COMMA))
	has_toplevel_comma = true;
      // Record the first two tokens after the "[".
      if (meaningful == 0)
	t1 = *t;
      else if (meaningful == 1)
	t2 = *t;
      ++meaningful;
    }

  // Restore the token stream: leave the last-read token in token_ and unget
  // everything before it (see peek/advance/unget: ungot_ is a LIFO stack).
  for (int i = (int) saved.size() - 2; i >= 0; --i)
    this->unget_token(saved[i]);

  if (!t1.is_identifier())
    return false;
  // "[" ident "]" is an array (e.g. "[N]T"), not a type parameter list.
  if (t2.is_op(OPERATOR_RSQUARE) || t2.is_invalid())
    return false;
  if (t2.is_identifier()
      || t2.is_op(OPERATOR_COMMA)
      || t2.is_op(OPERATOR_TILDE)
      || t2.is_op(OPERATOR_LSQUARE)
      || t2.is_op(OPERATOR_CHANOP)
      || t2.is_keyword(KEYWORD_INTERFACE)
      || t2.is_keyword(KEYWORD_CHAN)
      || t2.is_keyword(KEYWORD_FUNC)
      || t2.is_keyword(KEYWORD_MAP)
      || t2.is_keyword(KEYWORD_STRUCT))
    return true;
  // Ambiguous starts (e.g. "*" for a pointer term, "(" for a parenthesized
  // constraint): it is a type parameter list only if the bracket has a
  // top-level "|" (union constraint) or "," (multiple parameters), which an
  // array length expression never has.
  if (has_toplevel_pipe || has_toplevel_comma)
    return true;
  return false;
}

// Generics: capture a generic type template.  The current token is the
// "[" that introduces the type parameter list.  We record the type
// parameter names and the tokens of the type definition (up to the
// terminating semicolon), then register the template; no type is
// defined here.

void
Parse::generic_type_decl(const std::string& name, bool is_exported,
			 Location location)
{
  Generic_function_info* info =
    new Generic_function_info(name, is_exported, location);

  // A generic type declared inside a function body is function-local; its
  // instances get in-function identity rather than the cross-package
  // canonical-instance registry (see Generic_function_info::is_function_local
  // and instantiate_generic_type).
  if (!this->gogo_->in_global_scope())
    {
      info->set_is_function_local();
      // Remember the declaration contour so the instance body is later replayed
      // resolving generic-type names in the lexical scope where the template
      // was declared, not the use site (see instantiate_generic_type).
      info->set_decl_bindings(this->gogo_->current_bindings_for_generics());
      // If declared inside a GENERIC function, record that enclosing
      // instantiation's type arguments, for the instance's reflection name
      // ("Base[enclosingArgs;ownArgs]").
      if (!enclosing_generic_args_stack.empty())
	info->set_enclosing_type_args(enclosing_generic_args_stack.back());
    }

  this->type_parameter_names(&info->type_param_names(), &info->constraints());

  // Note any package qualifier appearing only in a constraint (see the
  // generic function path for why).
  for (size_t ci = 0; ci < info->constraints().size(); ++ci)
    this->note_token_package_usage(info->constraints()[ci],
				   &info->package_aliases());

  // Capture the type definition tokens up to a top-level semicolon.
  std::vector<Token>& toks = info->tokens();
  int depth = 0;
  while (true)
    {
      const Token* t = this->peek_token();
      if (t->is_eof())
	break;
      if (depth == 0 && t->is_op(OPERATOR_SEMICOLON))
	break;
      toks.push_back(*t);
      if (t->is_op(OPERATOR_LPAREN) || t->is_op(OPERATOR_LSQUARE)
	  || t->is_op(OPERATOR_LCURLY))
	++depth;
      else if (t->is_op(OPERATOR_RPAREN) || t->is_op(OPERATOR_RSQUARE)
	       || t->is_op(OPERATOR_RCURLY))
	--depth;
      this->advance_token();
    }
  toks.push_back(Token::make_eof_token(location));
  this->note_token_package_usage(toks, &info->package_aliases());

  // Register under the packed name so that lookups in type context
  // (which pack the name) match, including for unexported types.  A
  // function-local generic type goes into the current block/function bindings
  // (lexically scoped); a package-level one into the global registry.
  std::string packed = this->gogo_->pack_hidden_name(name, is_exported);
  if (info->is_function_local())
    this->gogo_->add_local_generic_type(packed, info);
  else
    this->gogo_->add_generic_type(packed, info);
}

// Generics: is NAME a predeclared/universal type name?  Such a name denotes the
// same type in every package, so it needs no pkgpath qualification in a generic
// instance's canonical id (unlike a package-local type name).

static bool
is_predeclared_generic_arg_name(const std::string& name)
{
  static const char* const names[] = {
    "bool", "byte", "rune", "string", "error", "any", "comparable",
    "int", "int8", "int16", "int32", "int64",
    "uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
    "float32", "float64", "complex64", "complex128"
  };
  for (size_t i = 0; i < sizeof(names) / sizeof(names[0]); ++i)
    if (name == names[i])
      return true;
  return false;
}

// Generics: see the declaration in parse.h.

std::string
Parse::generic_instance_canonical_id(Generic_function_info* info,
			      const std::vector<std::vector<Token> >& type_args,
			      const std::map<std::string, std::string>& aliases)
{
  // Build the id purely from the (already alias-canonicalized) argument
  // tokens -- no type resolution, so this neither pollutes bindings nor
  // depends on predeclared names being connected yet.  A package qualifier
  // "alias . Name" is rewritten to "<pkgpath> . Name" via ALIASES so the id
  // is package-independent; other tokens are kept verbatim.  A bare
  // (unqualified) argument keeps its spelling: such an argument is either a
  // predeclared type (universal) or a package-local type (an instance only
  // meaningful within one package), so this does not cause cross-package
  // mismatches for the qualified arguments that need unifying.
  std::string origin = (info->defining_package() != NULL
			? info->defining_package()->pkgpath()
			: this->gogo_->pkgpath());
  std::string id = origin + "." + Gogo::unpack_hidden_name(info->name()) + "[";
  for (size_t i = 0; i < type_args.size(); ++i)
    {
      if (i > 0)
	id += ",";
      const std::vector<Token>& arg = type_args[i];
      for (size_t j = 0; j < arg.size(); ++j)
	{
	  if (arg[j].is_identifier()
	      && j + 1 < arg.size()
	      && arg[j + 1].is_op(OPERATOR_DOT))
	    {
	      std::map<std::string, std::string>::const_iterator a =
		aliases.find(arg[j].identifier());
	      if (a != aliases.end())
		{
		  // Emit "<pkgpath>.<Name>" with a LITERAL dot, matching the
		  // bare-local-name branch below (which appends pkgpath + "." +
		  // name).  Otherwise a qualified "pkg.T" argument would encode
		  // the "." via token_key_string ("o39") while a bare "T" uses a
		  // literal ".", giving two different canonical ids for the same
		  // type -- e.g. an inferred cross-package arg ("lib.Pub", from
		  // type_to_tokens) vs the same instance spelled with a bare
		  // "Pub" in its own package (a type alias's arguments).  Skip
		  // the following "." token.
		  id += a->second;   // pkgpath, not the (ambiguous) alias
		  id += ".";
		  ++j;               // skip the "." (emitted as a literal above)
		  continue;
		}
	    }
	  // A bare (unqualified) identifier that names a package-local type must
	  // be qualified with the compiling package's pkgpath: two packages that
	  // each instantiate the same imported generic with a same-spelled local
	  // type (e.g. both define "errArrayElem" and instantiate
	  // "pool.Pool[*errArrayElem]") are DIFFERENT instances and must not
	  // collide on one canonical id.  A predeclared/universal name (int,
	  // string, error, ...) is the same type everywhere and is left as-is;
	  // so is the "Name" half of a "pkg.Name" qualifier (preceded by a dot)
	  // and any transient "$..."-marker identifier.
	  if (arg[j].is_identifier()
	      && (j == 0 || !arg[j - 1].is_op(OPERATOR_DOT))
	      && !(j + 1 < arg.size() && arg[j + 1].is_op(OPERATOR_DOT))
	      && arg[j].identifier()[0] != '$'
	      && !is_predeclared_generic_arg_name(arg[j].identifier()))
	    {
	      id += this->gogo_->pkgpath();
	      id += ".";
	      id += arg[j].identifier();
	      continue;
	    }
	  id += token_key_string(arg[j]);
	}
    }
  id += "]";
  return id;
}

// Generics: instantiate a generic type template with the given type
// arguments.  Returns the instance type (possibly a forward
// declaration during recursive instantiation).

Type*
Parse::instantiate_generic_type(Generic_function_info* info,
				const std::vector<std::vector<Token> >& type_args,
				Location location,
				const std::map<std::string, std::string>*
				  extra_pkg_aliases)
{
  // The alias->pkgpath map for re-parsing this instance: the template's own
  // imports, plus the caller's bindings for the type-argument packages (so a
  // cross-package type argument resolves to the exact package even when a
  // same-named package -- e.g. the ubiquitous "internal" -- is in scope).
  std::map<std::string, std::string> merged_aliases = info->package_aliases();
  if (extra_pkg_aliases != NULL)
    merged_aliases.insert(extra_pkg_aliases->begin(), extra_pkg_aliases->end());

  // Build a mangled key from the type arguments and check the cache.  Key by
  // resolved pkgpath (via merged_aliases) so same-spelling/different-package
  // type arguments do not collide on one cache entry.
  std::string key = this->instance_key(type_args);
  Named_object* cached = info->find_instance(key);
  if (cached != NULL)
    {
      if (cached->is_type_declaration())
	return Type::make_forward_declaration(cached);
      return cached->type_value();
    }

  if (type_args.size() != info->type_param_names().size())
    {
      go_error_at(location,
		  "wrong number of type arguments for generic type %qs",
		  Gogo::message_name(info->name()).c_str());
      return Type::make_error_type();
    }

  // If any type argument is an inference marker ("$infermarkerK"), this is a
  // transient instance built only to give a marker signature its shape for
  // unification (e.g. the result type "TPM2B[T, P]" of go-tpm's New2B while its
  // own type arguments are still being inferred).  Its arguments are markers,
  // not real types, so recording constraint obligations for them would check a
  // "$infermarkerK" against the constraint and spuriously fail (e.g. a marker
  // does not satisfy "Marshallable").  Skip.
  bool marker_arg_ct = false;
  for (size_t i = 0; i < type_args.size() && !marker_arg_ct; ++i)
    for (size_t j = 0; j < type_args[i].size(); ++j)
      if (type_args[i][j].is_identifier()
	  && type_args[i][j].identifier().compare(0, 12, "$infermarker") == 0)
	{
	  marker_arg_ct = true;
	  break;
	}
  if (!marker_arg_ct)
    record_constraint_obligations(this->gogo_, info, type_args,
				  Gogo::message_name(info->name()), location,
				  extra_pkg_aliases);

  // Substitute type arguments for type parameter names throughout the
  // captured token stream.
  std::vector<Token> substituted;
  substitute_type_params(info->tokens(), info->type_param_names(), type_args,
			 substituted);

  // Make a unique instance name.
  static unsigned int count;
  char buf[64];
  snprintf(buf, sizeof buf, "$type%u", count);
  ++count;
  std::string iname = info->name() + std::string(buf);
  std::string packed = this->gogo_->pack_hidden_name(iname, false);

  // A function-local generic type is instantiated in-place, inside the
  // enclosing function's scope: we must NOT push_instantiation_context (which
  // saves and clears the function stack, making the instance a package-level
  // type), so that declare_type below records the instance as in-function and
  // its backend/reflection name incorporates the (distinct) enclosing function
  // instance.  It also must NOT join the cross-package canonical-instance
  // registry (canon_id is forced empty below), so F[string].Box[int] and
  // F[float64].Box[int] stay distinct nominal types.
  bool local = info->is_function_local();
  if (!local)
    this->gogo_->push_instantiation_context();
  bool imported = info->defining_package() != NULL;
  if (imported)
    this->gogo_->push_instantiation_package(info->defining_package());

  // Compute the package-independent canonical id (inside the instantiation
  // context so predeclared/defining-package names resolve) and consult the
  // global canonical-instance registry, so a locally-created instance and one
  // imported by reference (or created while importing another package) unify
  // to a single nominal type object across packages.  Skipped for a
  // function-local generic type (see above): forcing an empty id disables the
  // registry lookup/registration and set_generic_canonical_id below.
  std::string canon_id = local
    ? std::string()
    : this->generic_instance_canonical_id(info, type_args, merged_aliases);
  if (!canon_id.empty())
    {
      Named_object* cno =
	this->gogo_->lookup_canonical_generic_instance(canon_id);
      if (cno != NULL)
	{
	  // Decide whether we can reuse this canonical instance directly.  A
	  // canonical instance imported from another package (package() != NULL)
	  // that carries methods must NOT be reused here: we cannot attach this
	  // package's method instantiations to a non-local type ("may not define
	  // methods on non-local type"), and a local use needs a local instance
	  // that owns its own method set.  In that case fall through and build a
	  // fresh local instance, which overwrites the canonical registration for
	  // the rest of this compilation.  Method-less instances (func-type or
	  // plain type-alias instances) are always safe to reuse, and MUST be
	  // reused so we do not needlessly emit a second structurally-identical
	  // unnamed underlying type (the export-time Sort_types alias-identity
	  // check tolerates any residual duplicates) when both the imported and a
	  // local instance are reachable from the export set.  Reusing here keeps
	  // the common func-type/alias case down to one instance.  The method
	  // check is reliable here:
	  // instantiation runs while parsing this package's source, after all
	  // imports (and their late finalize_methods()) have completed.
	  bool can_reuse = true;
	  if (!cno->is_type_declaration() && cno->package() != NULL)
	    {
	      Named_type* cnt = cno->type_value()->named_type();
	      if (cnt != NULL && cnt->has_any_methods())
		can_reuse = false;
	    }
	  if (can_reuse)
	    {
	      // Share under this template's local token key too (fast path).
	      info->add_instance(key, cno, type_args);
	      if (imported)
		this->gogo_->pop_instantiation_package();
	      this->gogo_->pop_instantiation_context();
	      if (cno->is_type_declaration())
		return Type::make_forward_declaration(cno);
	      return cno->type_value();
	    }
	}
    }

  // Declare the instance type first so that recursive references to the
  // same instantiation resolve to it.
  Named_object* no = this->gogo_->declare_type(packed, location);
  info->add_instance(key, no, type_args);
  // Register under the canonical id so any other package's instantiation of
  // the same generic with the same argument identities reuses THIS object.
  if (!canon_id.empty())
    this->gogo_->add_canonical_generic_instance(canon_id, no);

  // A PACKAGE-scope generic type's body (and its methods) is lexically at
  // package scope and must resolve generic-type names there, never against a
  // caller's function-local generic templates -- but that is now guaranteed by
  // scoping: function-local templates live in the function's Bindings, and this
  // replay runs with the function stack cleared (push_instantiation_context),
  // so lookup_generic_type does not consult them.  (A function-local generic
  // type's OWN instantiation, `local`, keeps the function stack, so it still
  // sees its sibling function-local types.)
  Parse ip(this->lex_, this->gogo_);
  ip.set_replay_tokens(&substituted);
  ip.set_replay_pkg_aliases(&merged_aliases);
  // For a function-local generic type, resolve generic-type names in its
  // DECLARATION contour, not the use site (so an inner-block same-named type at
  // the use site does not shadow the type visible where the template was
  // declared).
  bool pushed_lex = false;
  if (local && info->decl_bindings() != NULL)
    {
      this->gogo_->push_replay_lexical_scope(info->decl_bindings());
      pushed_lex = true;
    }
  Type* underlying = ip.type();
  if (pushed_lex)
    this->gogo_->pop_replay_lexical_scope();

  Named_type* nt = Type::make_named_type(no, underlying, location);
  // Record the canonical id on the instance so it exports and so nested
  // instances referencing it as an argument reuse its origin-based identity.
  if (!canon_id.empty())
    nt->set_generic_canonical_id(canon_id);
  // Record the generic's source name so an embedded field of this instance
  // is named for the generic (e.g. "Box"), not the instance ("Box$type0").
  std::string base_name = Gogo::unpack_hidden_name(info->name());
  nt->set_generic_base_name(base_name);
  // For an unexported generic, an embedded field of this instance must use
  // the package-hidden name (".pkg.box"), matching how a selector packs the
  // field name (with the defining package's pkgpath while instantiating).
  if (!Lex::is_exported_name(base_name))
    nt->set_generic_embedded_field_name(
      this->gogo_->pack_hidden_name_for_field(base_name, false));
  // Record the resolved type arguments so the instance reflects as
  // "Base[arg0,arg1,...]" rather than its mangled instance name.
  {
    std::vector<Type*> argtypes;
    bool all_ok = true;
    for (size_t i = 0; i < type_args.size(); ++i)
      {
	Type* at = this->parse_type_from_tokens(type_args[i], &merged_aliases);
	if (at == NULL || at->is_error_type())
	  {
	    all_ok = false;
	    break;
	  }
	argtypes.push_back(at);
      }
    if (all_ok)
      nt->set_generic_type_args(argtypes);
  }
  // For a function-local generic type declared in a generic function, record
  // the enclosing instantiation's type args too, so the instance reflects as
  // "Base[enclosingArgs;ownArgs]" (matching gc) and instances differing only
  // in the enclosing args are distinct reflect.Types.
  if (local && !info->enclosing_type_args().empty())
    {
      std::vector<Type*> encl;
      bool all_ok = true;
      for (size_t i = 0; i < info->enclosing_type_args().size(); ++i)
	{
	  Type* et = this->parse_type_from_tokens(info->enclosing_type_args()[i],
						  &merged_aliases, false);
	  if (et == NULL || et->is_error_type())
	    {
	      all_ok = false;
	      break;
	    }
	  encl.push_back(et);
	}
      if (all_ok)
	nt->set_generic_enclosing_type_args(encl);
    }
  this->gogo_->define_type(no, nt);

  // Record this instance's canonical spelling ("Name[arg, ...]") so that
  // type inference can express it as a type argument in a way that hashes
  // to the same instance as writing the type out directly.  The type
  // arguments are already canonical: written-out args are source tokens,
  // and inferred args come from type_to_tokens, which emits the recorded
  // spelling for any instance.
  {
    std::vector<Token> spelling;
    // A generic type imported from another package must be spelled with its
    // package qualifier ("pkg.Name"), so that when this spelling is later
    // replayed as an inferred type argument (e.g. "reset(h.pts)" infers
    // T = metricdata.DataPoint[N]) the base name still resolves.  A bare
    // "DataPoint[int64]" would be undefined in the re-parsing package.
    Package* dpkg = info->defining_package();
    std::string base = info->name();
    bool base_hidden = Gogo::is_hidden_name(base);
    std::string base_src =
      base_hidden ? Gogo::unpack_hidden_name(base) : base;
    bool base_exported =
      base_hidden ? false : Lex::is_exported_name(base_src);
    if (dpkg != NULL
	&& dpkg->has_package_name()
	&& dpkg->pkgpath() != this->gogo_->pkgpath())
      {
	spelling.push_back(
	  Token::make_identifier_token(dpkg->package_name(), false, location));
	spelling.push_back(Token::make_operator_token(OPERATOR_DOT, location));
      }
    spelling.push_back(
      Token::make_identifier_token(base_src, base_exported, location));
    spelling.push_back(Token::make_operator_token(OPERATOR_LSQUARE, location));
    for (size_t i = 0; i < type_args.size(); ++i)
      {
	if (i > 0)
	  spelling.push_back(Token::make_operator_token(OPERATOR_COMMA,
							location));
	for (size_t j = 0; j < type_args[i].size(); ++j)
	  spelling.push_back(type_args[i][j]);
      }
    spelling.push_back(Token::make_operator_token(OPERATOR_RSQUARE, location));
    generic_instance_spelling[no] = spelling;
  }

  this->instantiate_instance_methods(info, type_args, nt, underlying, location);

  if (imported)
    this->gogo_->pop_instantiation_package();
  if (!local)
    this->gogo_->pop_instantiation_context();

  return nt;
}

// Generics: instantiate (once) the methods of the generic instance NT onto its
// Named_object, from INFO's method templates substituted with TYPE_ARGS.  The
// caller must have pushed the instantiation context/package.  Idempotent: an
// instance's methods are instantiated at most once per compilation, so this is
// safe to call both when creating an instance and when reusing an imported
// instance whose defining package never instantiated its method set.

void
Parse::instantiate_instance_methods(Generic_function_info* info,
				    const std::vector<std::vector<Token> >&
				      type_args,
				    Named_type* nt, Type* underlying,
				    Location location)
{
  // Do this at most once per instance object.
  static std::set<Named_object*> done;
  if (nt->named_object() != NULL
      && !done.insert(nt->named_object()).second)
    return;

  // If any type argument is an inference marker ("$infermarkerK"), this is a
  // transient instance built only to give a marker signature its shape for
  // unification (e.g. a parameter of type "T[N]" while inferring the call's
  // own type arguments).  Its methods are not needed and would fail to parse
  // against the marker, so skip instantiating them.
  bool marker_arg = false;
  for (size_t i = 0; i < type_args.size() && !marker_arg; ++i)
    for (size_t j = 0; j < type_args[i].size(); ++j)
      if (type_args[i][j].is_identifier()
	  && type_args[i][j].identifier().compare(0, 12, "$infermarker") == 0)
	{
	  marker_arg = true;
	  break;
	}

  // Instantiate the type's methods: for each method template, substitute
  // the receiver's type parameter names with the type arguments and
  // re-parse it as an ordinary method on this instance.
  //
  // Push the template's defining package so that bare type names in a method
  // body (and in type arguments to any nested generic instantiated by that
  // body, e.g. Address in iter.Seq2[Address, T] inside a method of an imported
  // resolver.AddressMapV2[T]) resolve against the defining package rather than
  // being left as unresolved forward declarations in the importing package.
  Package* method_defpkg = info->defining_package();
  if (method_defpkg != NULL)
    this->gogo_->push_instantiation_package(method_defpkg);
  for (size_t m = 0; !marker_arg && m < info->methods().size(); ++m)
    {
      Generic_method_template& mt = info->methods()[m];
      std::vector<Token> msubst;
      substitute_type_params(mt.tokens, mt.recv_type_param_names, type_args,
			     msubst);
      this->instantiate_generic_method(msubst, location,
				       &info->package_aliases());
    }
  if (method_defpkg != NULL)
    this->gogo_->pop_instantiation_package();

  // When the type is instantiated during a late pass (e.g. type-argument
  // inference), the global finalize_methods pass has already run, so build
  // this instance's method table now; otherwise a following method call
  // would not find the freshly added methods.  This is needed not only when
  // the generic has its own methods but also when its underlying is a struct
  // that promotes methods from an embedded (possibly generic) field.
  if (this->gogo_->parsing_complete()
      && (!info->methods().empty() || underlying->struct_type() != NULL))
    nt->finalize_methods(this->gogo_);

  // A generic interface instance (e.g. Iterator[int]) has its method set
  // in the interface body rather than as method templates, so the block
  // above does not cover it.  When created during a late pass, finalize
  // its method set now so that a later method lookup (find_method, which
  // asserts the methods are finalized) does not crash.
  if (this->gogo_->parsing_complete())
    {
      Interface_type* uit = underlying->interface_type();
      if (uit != NULL)
	uit->finalize_methods();
    }
}

// Generics: parse a "[type-args]" list at a use site of a generic type
// and return the resulting instance type.  The current token is "[".

Type*
Parse::generic_type_instantiation(Generic_function_info* info,
				  Location location)
{
  go_assert(this->peek_token()->is_op(OPERATOR_LSQUARE));
  this->advance_token();

  std::vector<std::vector<Token> > type_args;
  while (!this->peek_token()->is_op(OPERATOR_RSQUARE)
	 && !this->peek_token()->is_eof())
    {
      std::vector<Token> arg;
      int depth = 0;
      while (true)
	{
	  const Token* t = this->peek_token();
	  if (t->is_eof())
	    break;
	  if (depth == 0
	      && (t->is_op(OPERATOR_COMMA) || t->is_op(OPERATOR_RSQUARE)))
	    break;
	  if (t->is_op(OPERATOR_LSQUARE) || t->is_op(OPERATOR_LPAREN)
	      || t->is_op(OPERATOR_LCURLY))
	    ++depth;
	  else if (t->is_op(OPERATOR_RSQUARE) || t->is_op(OPERATOR_RPAREN)
		   || t->is_op(OPERATOR_RCURLY))
	    --depth;
	  arg.push_back(*t);
	  this->advance_token();
	}
      type_args.push_back(arg);
      if (this->peek_token()->is_op(OPERATOR_COMMA))
	this->advance_token();
    }
  // Consume "]".
  this->advance_token();

  // A function-local type argument is out of scope by the time the generic
  // type is re-parsed, so rewrite any such argument to its stable synthetic
  // "$localtypeN" spelling while the declaration scope is still active.
  this->localize_local_type_args(type_args);

  // Record the package-qualifier bindings of the type arguments from the
  // current (correct) scope, so the instance re-parse resolves each qualifier
  // to the exact package the argument came from -- even when a same-named
  // package (e.g. "internal") is imported elsewhere or is the template's own
  // package name.
  std::map<std::string, std::string> pkg_bindings;
  for (size_t i = 0; i < type_args.size(); ++i)
    this->note_token_package_usage(type_args[i], &pkg_bindings);

  // Canonicalize type-alias arguments so the instance identity is stable.  If
  // any argument is a still-unresolved forward reference (e.g. a package-level
  // alias declared in another file, not yet seen during this parse), its
  // canonical identity cannot be computed yet: defer the instantiation to the
  // post-parse pending-resolution pass rather than create a final instance
  // under a non-canonical spelling (which would not unify with the canonical
  // one and would break cross-package instance identity).
  bool all_resolved = this->canonicalize_type_args(type_args, pkg_bindings,
						   location);
  if (!all_resolved && !this->gogo_->parsing_complete())
    {
      static unsigned int count;
      char buf[64];
      snprintf(buf, sizeof buf, "$pendinginst%u", count);
      ++count;
      std::string phname =
	this->gogo_->pack_hidden_name(std::string(buf), false);
      Named_object* placeholder = this->gogo_->declare_type(phname, location);
      Pending_generic_type* p = new Pending_generic_type;
      p->placeholder = placeholder;
      p->info = info;
      p->type_args = type_args;
      // Capture the qualifier bindings now, while they still resolve: at the
      // post-parse resolution pass the file-scope imports and replay aliases
      // are gone, so a by-name recomputation could pick the wrong same-named
      // package.
      p->pkg_bindings = pkg_bindings;
      p->location = location;
      this->gogo_->add_pending_generic_type(p);
      return Type::make_forward_declaration(placeholder);
    }

  return this->instantiate_generic_type(info, type_args, location,
					&pkg_bindings);
}

// Generics: see the declaration in parse.h.

bool
Parse::canonicalize_type_args(std::vector<std::vector<Token> >& type_args,
			      std::map<std::string, std::string>& pkg_bindings,
			      Location location)
{
  bool all_resolved = true;
  for (size_t i = 0; i < type_args.size(); ++i)
    {
      Type* t = this->parse_type_from_tokens(type_args[i], &pkg_bindings,
					     /*issue_error=*/false);
      if (t == NULL || t->forwarded()->forward_declaration_type() != NULL)
	{
	  // An unresolved forward reference: identity cannot be finalized yet.
	  all_resolved = false;
	  continue;
	}
      Type* fwd = t->forwarded();
      // Rewrite a TYPE ALIAS argument to its underlying type's canonical
      // spelling, so "Factory[Request]" (type Request = pkg.Request) keys and
      // spells identically to "Factory[pkg.Request]".  gccgo compares generic
      // instances nominally and unifies cross-package instances by matching
      // their type-argument spelling; without this an alias and its underlying
      // name produce two distinct, non-identical instances of one generic.
      if (fwd->named_type() != NULL && fwd->named_type()->is_alias())
	{
	  std::vector<Token> canon;
	  std::map<std::string, std::string> canon_bindings;
	  if (type_to_tokens(fwd->unalias(), canon, location, &canon_bindings)
	      && !canon.empty())
	    {
	      type_args[i] = canon;
	      pkg_bindings.insert(canon_bindings.begin(), canon_bindings.end());
	    }
	}
    }
  return all_resolved;
}

// Generics: a use of a generic type before its declaration.  Parse the
// "[type-args]" list, create a placeholder type declaration, record a
// pending instantiation, and return a forward declaration of the
// placeholder.  The placeholder is resolved (made an alias of the real
// instantiation) after all input is parsed.

Type*
Parse::pending_generic_type_instantiation(const std::string& name,
					  Location location)
{
  go_assert(this->peek_token()->is_op(OPERATOR_LSQUARE));
  this->advance_token();

  std::vector<std::vector<Token> > type_args;
  while (!this->peek_token()->is_op(OPERATOR_RSQUARE)
	 && !this->peek_token()->is_eof())
    {
      std::vector<Token> arg;
      int depth = 0;
      while (true)
	{
	  const Token* t = this->peek_token();
	  if (t->is_eof())
	    break;
	  if (depth == 0
	      && (t->is_op(OPERATOR_COMMA) || t->is_op(OPERATOR_RSQUARE)))
	    break;
	  if (t->is_op(OPERATOR_LSQUARE) || t->is_op(OPERATOR_LPAREN)
	      || t->is_op(OPERATOR_LCURLY))
	    ++depth;
	  else if (t->is_op(OPERATOR_RSQUARE) || t->is_op(OPERATOR_RPAREN)
		   || t->is_op(OPERATOR_RCURLY))
	    --depth;
	  arg.push_back(*t);
	  this->advance_token();
	}
      type_args.push_back(arg);
      if (this->peek_token()->is_op(OPERATOR_COMMA))
	this->advance_token();
    }
  // Consume "]".
  this->advance_token();

  // A function-local type argument is out of scope by the time the generic
  // type is re-parsed, so rewrite any such argument to its stable synthetic
  // "$localtypeN" spelling while the declaration scope is still active.
  this->localize_local_type_args(type_args);

  std::map<std::string, std::string> pkg_bindings;
  for (size_t i = 0; i < type_args.size(); ++i)
    this->note_token_package_usage(type_args[i], &pkg_bindings);

  return this->make_pending_generic_type(name, type_args, location,
					 &pkg_bindings);
}

// Generics: create a placeholder type and record a pending instantiation
// of generic type NAME with the given (already-parsed) TYPE_ARGS, to be
// resolved after all input is parsed.  Returns a forward declaration of
// the placeholder.

Type*
Parse::make_pending_generic_type(const std::string& name,
				 const std::vector<std::vector<Token> >& type_args,
				 Location location,
				 const std::map<std::string, std::string>*
				   pkg_bindings)
{
  static unsigned int count;
  char buf[64];
  snprintf(buf, sizeof buf, "$pendinggen%u", count);
  ++count;
  // Name the placeholder within the current package -- a proper hidden name
  // ".pkgpath.$pendinggenN" rather than the bare ".$pendinggenN".  If the
  // placeholder alias reaches export data (referenced by an exported generic
  // instance type), an importer splits its name into (pkgpath, name); a bare
  // ".$pendinggenN" mis-splits into a phantom package named "$pendinggenN"
  // that has no package name and crashes the exporter of the importing
  // package.  Qualifying with the real pkgpath (as the generic instance types
  // themselves are named) makes the importer attribute it to this package.
  std::string phname = this->gogo_->pack_hidden_name(std::string(buf), false);
  Named_object* placeholder = this->gogo_->declare_type(phname, location);

  // Resolve the name to the key under which the generic type is registered,
  // now (while any instantiation-package context is still in effect) rather
  // than after parsing, when it is lost.  A bare reference inside an imported
  // template body names one of the defining package's own generic types.
  std::string gname = name;
  if (this->gogo_->lookup_generic_type(gname) == NULL)
    {
      Package* ip = this->gogo_->current_instantiation_package();      if (ip != NULL)
	{
	  std::string key = ip->pkgpath() + '.' + Gogo::unpack_hidden_name(name);	  if (this->gogo_->lookup_generic_type(key) != NULL)
	    gname = key;
	}
    }

  Pending_generic_type* p = new Pending_generic_type;
  p->placeholder = placeholder;
  p->generic_name = gname;
  p->info = NULL;
  p->type_args = type_args;
  if (pkg_bindings != NULL)
    p->pkg_bindings = *pkg_bindings;
  p->location = location;
  this->gogo_->add_pending_generic_type(p);

  return Type::make_forward_declaration(placeholder);
}

// Generics: resolve all recorded forward references to generic types.
// Called after all input has been parsed and all templates registered.

void
Parse::resolve_pending_generic_types()
{
  std::vector<Pending_generic_type*>& pend =
    this->gogo_->pending_generic_types();
  for (size_t i = 0; i < pend.size(); ++i)
    {
      Pending_generic_type* p = pend[i];

      // Deferred because a TYPE ARGUMENT was a forward reference at parse time
      // (the template itself was known).  The argument should resolve now, so
      // re-canonicalize the arguments and instantiate via the known template,
      // then redirect the placeholder to the canonical instance.
      if (p->info != NULL)
	{
	  std::vector<std::vector<Token> > targs = p->type_args;
	  std::map<std::string, std::string> bindings;
	  for (size_t k = 0; k < targs.size(); ++k)
	    this->note_token_package_usage(targs[k], &bindings);
	  // The bindings captured at the deferral site (while file-scope imports
	  // were in scope) are authoritative: they disambiguate a qualifier that
	  // is ambiguous by name here, so let them win over the by-name recompute.
	  for (std::map<std::string, std::string>::const_iterator b =
		 p->pkg_bindings.begin(); b != p->pkg_bindings.end(); ++b)
	    bindings[b->first] = b->second;
	  this->canonicalize_type_args(targs, bindings, p->location);
	  Type* inst = this->instantiate_generic_type(p->info, targs,
						      p->location, &bindings);
	  Named_type* alias = Type::make_named_type(p->placeholder, inst,
						    p->location);
	  alias->set_is_alias();
	  Named_type* inst_nt = inst->named_type();
	  if (inst_nt != NULL)
	    {
	      if (!inst_nt->generic_canonical_id().empty())
		alias->set_generic_canonical_id(inst_nt->generic_canonical_id());
	      if (!inst_nt->generic_type_args().empty())
		alias->set_generic_type_args(inst_nt->generic_type_args());
	    }
	  std::string base_name = Gogo::unpack_hidden_name(p->info->name());
	  alias->set_generic_base_name(base_name);
	  if (!Lex::is_exported_name(base_name))
	    alias->set_generic_embedded_field_name(
	      this->gogo_->pack_hidden_name_for_field(base_name, false));
	  this->gogo_->define_type(p->placeholder, alias);
	  continue;
	}

      Generic_function_info* info =
	this->gogo_->lookup_generic_type(p->generic_name);
      if (info == NULL)
	{
	  // A generic type referenced before its declaration is recorded
	  // under the (raw) source name, but templates are registered under
	  // their packed hidden name.  For an unexported generic type those
	  // differ (".pkgpath.name" vs "name"), so retry with the packed
	  // name.
	  std::string bare = Gogo::unpack_hidden_name(p->generic_name);
	  std::string packed =
	    this->gogo_->pack_hidden_name(bare, Lex::is_exported_name(bare));
	  info = this->gogo_->lookup_generic_type(packed);
	}
      if (info == NULL)
	{	  go_error_at(p->location, "reference to undefined generic type");
	  continue;
	}
      // Canonicalize the (raw) type arguments before instantiating, exactly as
      // the p->info != NULL branch above does.  These arguments were captured
      // as source tokens when the generic type NAME was still a forward
      // reference, so they were never canonicalized.  Without this, an argument
      // that is a type alias -- notably the predeclared "any" (an alias for
      // "interface{}") -- keeps its alias spelling and produces a different
      // canonical instance id than the same instantiation written inline (which
      // is canonicalized to "interface{}").  That yields two distinct instance
      // objects for one type; a later re-parse of one instance's method
      // receiver canonicalizes to the OTHER object and re-adds the methods
      // there, causing a spurious "redefinition of <method>" error.
      std::vector<std::vector<Token> > targs = p->type_args;
      std::map<std::string, std::string> bindings;
      for (size_t k = 0; k < targs.size(); ++k)
	this->note_token_package_usage(targs[k], &bindings);
      for (std::map<std::string, std::string>::const_iterator b =
	     p->pkg_bindings.begin(); b != p->pkg_bindings.end(); ++b)
	bindings[b->first] = b->second;
      this->canonicalize_type_args(targs, bindings, p->location);
      Type* inst = this->instantiate_generic_type(info, targs,
						  p->location, &bindings);
      // Make the placeholder an alias of the real instantiation.
      Named_type* alias = Type::make_named_type(p->placeholder, inst,
						p->location);
      alias->set_is_alias();
      Named_type* inst_nt = inst->named_type();
      if (inst_nt != NULL)
	{
	  if (!inst_nt->generic_canonical_id().empty())
	    alias->set_generic_canonical_id(inst_nt->generic_canonical_id());
	  if (!inst_nt->generic_type_args().empty())
	    alias->set_generic_type_args(inst_nt->generic_type_args());
	}
      // If this instance is used as an embedded struct field before the
      // generic type is declared ("type S struct{ box[int] }" with box
      // declared later), the field name is derived from the alias by
      // Struct_field::field_name.  Record the generic's source name on the
      // alias so the field is named for the generic ("box"), not for the
      // placeholder ("$pendinggenN").  Mirrors instantiate_generic_type.
      {
	std::string base_name = Gogo::unpack_hidden_name(info->name());
	alias->set_generic_base_name(base_name);
	if (!Lex::is_exported_name(base_name))
	  alias->set_generic_embedded_field_name(
	    this->gogo_->pack_hidden_name_for_field(base_name, false));
      }
      this->gogo_->define_type(p->placeholder, alias);
    }
  this->gogo_->resolve_global_names();
}

// Generics: parse a captured token sequence as a type (used to evaluate
// constraints after types are determined).  Returns NULL on failure.

Type*
Parse::parse_type_from_tokens(const std::vector<Token>& toks,
			      const std::map<std::string, std::string>* aliases,
			      bool issue_error)
{
  std::vector<Token> t = toks;
  t.push_back(Token::make_eof_token(Linemap::unknown_location()));
  Parse p(this->lex_, this->gogo_);
  p.set_replay_tokens(&t);
  if (aliases != NULL && !aliases->empty())
    p.set_replay_pkg_aliases(aliases);
  if (!p.type_may_start_here())
    return NULL;
  return p.type(issue_error);
}

// Generics: resolve a constraint type-set element to a type, looking
// predeclared and package-global names up in the global bindings so that
// a name used only inside a constraint (and therefore not connected by a
// throwaway re-parse) still resolves.  Does not emit errors.

Type*
Parse::resolve_constraint_type(const std::vector<Token>& toks,
			       const std::map<std::string, std::string>* aliases)
{
  if (toks.size() == 1 && toks[0].is_identifier())
    {
      // Synthetic local-type names are resolved by the replay map; ordinary
      // name lookup would miss them because they are out of scope when the
      // instantiation is revisited.
      if (toks[0].identifier().compare(0, 10, "$localtype") == 0)
	{
	  std::map<std::string, Named_object*>::const_iterator p =
	    replay_local_type_obj.find(toks[0].identifier());
	  if (p != replay_local_type_obj.end() && p->second->is_type())
	    return p->second->type_value();
	  return NULL;
	}

      // Resolve a single name quietly (never via a throwaway re-parse,
      // which would emit a spurious "undefined type" error for a name that
      // belongs to another package).  Try the global bindings (for
      // predeclared types such as a "float64" used only in a constraint)
      // and the package bindings (for package-level named types).
      const std::string& nm = toks[0].identifier();
      std::string packed =
	this->gogo_->pack_hidden_name(nm, Lex::is_exported_name(nm));
      Named_object* no = this->gogo_->lookup_global(nm.c_str());
      if (no == NULL)
	no = this->gogo_->lookup_global(packed.c_str());
      if (no == NULL)
	no = this->gogo_->lookup(packed, NULL);
      if (no != NULL && no->is_type())
	return no->type_value();
      return NULL;
    }
  // Quietly: a constraint term whose package is not available in this
  // compilation (e.g. a union member's package that the instantiating package
  // does not import and that is not supplied via genimports) must yield NULL
  // so the constraint is left unenforced, not a spurious "undefined
  // identifier" error.  The argument still satisfies the constraint via a
  // resolvable member.
  return this->parse_type_from_tokens(toks, aliases, /*issue_error=*/false);
}

// Generics: see the declaration.  For constraint type inference, return
// the single structural type element of a constraint with type-parameter
// names replaced by inference markers, or NULL.

Type*
Parse::constraint_core_type_with_markers(const std::vector<Token>& c,
					 const std::vector<std::string>& names,
					 const std::vector<Type*>* solved,
					 const std::map<std::string, std::string>*
					   aliases)
{
  if (c.empty())
    return NULL;

  // The element tokens: strip an "interface { ... }" wrapper.
  std::vector<Token> elem;
  if (c[0].is_keyword(KEYWORD_INTERFACE))
    {
      if (c.size() < 2 || !c[1].is_op(OPERATOR_LCURLY))
	return NULL;
      int d = 1;
      size_t j = 2;
      for (; j < c.size(); ++j)
	{
	  if (c[j].is_op(OPERATOR_LCURLY))
	    ++d;
	  else if (c[j].is_op(OPERATOR_RCURLY))
	    {
	      --d;
	      if (d == 0)
		break;
	    }
	}
      elem.assign(c.begin() + 2, c.begin() + j);
    }
  else
    elem = c;

  // An interface body separates its elements with semicolons (newlines in
  // a multi-line body also become semicolons).  The core type comes only
  // from the single structural type element; method elements ("M(...)
  // ...") do not contribute and are ignored.  Split into elements, drop
  // the empty and method ones, and require exactly one structural element.
  {
    std::vector<std::vector<Token> > parts =
      split_top_level(elem, OPERATOR_SEMICOLON);
    const std::vector<Token>* structural = NULL;
    for (size_t i = 0; i < parts.size(); ++i)
      {
	const std::vector<Token>& p = parts[i];
	if (p.empty())
	  continue;
	// A method element is an identifier immediately followed by "(".
	if (p.size() >= 2 && p[0].is_identifier() && p[1].is_op(OPERATOR_LPAREN))
	  continue;
	// An embedded interface element -- a bare type name ("Unmarshallable")
	// or a qualified one ("pkg.Iface") that resolves to an interface type --
	// contributes only a method set, not a type term, so it carries no core
	// type and must be ignored (like a method element).  Constraints such as
	// "interface{ *T; Unmarshallable }" (go-tpm's New2B) otherwise wrongly
	// count the embedded interface as a second structural element, which
	// defeats core-type inference of the second type parameter (P = *T).
	// Only a bare/qualified name can be an embedded interface; a term with
	// leading "~", an operator, or a keyword (composite/pointer type) is
	// structural and handled below.
	{
	  bool name_only =
	    (p.size() == 1 && p[0].is_identifier())
	    || (p.size() == 3 && p[0].is_identifier()
		&& p[1].is_op(OPERATOR_DOT) && p[2].is_identifier());
	  if (name_only)
	    {
	      // Do not treat a type-parameter name as an embedded interface: it
	      // is a bare structural term (handled/rejected later).
	      bool is_type_param = false;
	      for (size_t n = 0; n < names.size() && !is_type_param; ++n)
		if (names[n] == p[0].identifier())
		  is_type_param = true;
	      if (!is_type_param)
		{
		  Type* pt = this->parse_type_from_tokens(p, aliases,
							  /*issue_error=*/false);
		  // Only classify as an embedded interface if the name actually
		  // resolved to a defined type.  A bare name from an imported
		  // template's constraint (e.g. "Marshallable", or a named
		  // type-set "Contents") may be an unresolved forward declaration
		  // here; calling interface_type() on it would force
		  // real_type()->warn() to emit a spurious "use of undefined type"
		  // error despite the quiet parse.  Leave such an element as a term
		  // rather than treating it as an embedded interface.
		  if (pt != NULL)
		    {
		      Forward_declaration_type* fdt =
			pt->forwarded()->forward_declaration_type();
		      if (fdt != NULL && !fdt->is_defined())
			;
		      else if (pt->forwarded()->interface_type() != NULL)
			continue;
		    }
		}
	    }
	}
	if (structural != NULL)
	  return NULL;
	structural = &p;
      }
    if (structural == NULL)
      return NULL;
    elem = *structural;
  }

  // A union of terms ("~int | ~string") has no single core type.
  int depth = 0;
  for (size_t i = 0; i < elem.size(); ++i)
    {
      const Token& t = elem[i];
      if (t.is_op(OPERATOR_LPAREN) || t.is_op(OPERATOR_LSQUARE)
	  || t.is_op(OPERATOR_LCURLY))
	++depth;
      else if (t.is_op(OPERATOR_RPAREN) || t.is_op(OPERATOR_RSQUARE)
	       || t.is_op(OPERATOR_RCURLY))
	--depth;
      else if (depth == 0 && t.is_op(OPERATOR_OR))
	return NULL;
    }

  size_t k = 0;
  if (!elem.empty() && elem[0].is_op(OPERATOR_TILDE))
    k = 1;
  if (k >= elem.size())
    return NULL;
  // A method-set element (e.g. "M()") is not a structural type term, so
  // it carries nothing to unify against for constraint type inference.
  // An identifier immediately followed by "(" can only be a method spec
  // here -- a structural term is a bare name, "Name[args]", or a
  // composite type (which begins with a keyword or operator, never
  // "identifier (").
  if (elem.size() - k >= 2
      && elem[k].is_identifier()
      && elem[k + 1].is_op(OPERATOR_LPAREN))
    return NULL;
  // A bare type name (e.g. just "T" or "int") carries no structure to
  // unify against, so it is useless for constraint type inference.
  if (elem.size() - k == 1 && elem[k].is_identifier())
    return NULL;

  // A named generic constraint "C[args]" (e.g. sliceOf[T] where
  // "type sliceOf[E any] interface{ ~[]E }"): expand C's definition with
  // its type parameters bound to the args, then take that constraint's
  // core type.
  if (elem.size() - k >= 3
      && elem[k].is_identifier()
      && elem[k + 1].is_op(OPERATOR_LSQUARE))
    {
      Generic_function_info* gi =
	this->gogo_->lookup_generic_type(
	  this->gogo_->pack_hidden_name(elem[k].identifier(),
					Lex::is_exported_name(elem[k].identifier())));
      if (gi != NULL)
	{
	  // Split the args between the outer "[" and matching "]".
	  std::vector<Token> inner(elem.begin() + k + 2, elem.end());
	  if (!inner.empty() && inner.back().is_op(OPERATOR_RSQUARE))
	    inner.pop_back();
	  std::vector<std::vector<Token> > gargs =
	    split_top_level(inner, OPERATOR_COMMA);
	  if (gargs.size() == gi->type_param_names().size())
	    {
	      std::vector<Token> sub;
	      substitute_type_params(gi->tokens(), gi->type_param_names(),
				     gargs, sub);
	      return this->constraint_core_type_with_markers(sub, names, solved,
							      aliases);
	    }
	}
    }

  // Substitute type-parameter names with inference markers.
  std::vector<Token> subst;
  // Package bindings for any cross-package solved types emitted below, so the
  // re-parse resolves their qualifiers to the exact package.  Seed with the
  // template's own package-qualifier aliases so that a constraint written with
  // a package qualifier (e.g. "cmp.Ordered") resolves that qualifier to the
  // package the template was defined against, rather than a same-named local
  // object (e.g. a local "func cmp") in the instantiating package.
  std::map<std::string, std::string> pkg_bindings;
  if (aliases != NULL)
    pkg_bindings.insert(aliases->begin(), aliases->end());
  for (size_t i = k; i < elem.size(); ++i)
    {
      const Token& t = elem[i];
      int which = -1;
      if (t.is_identifier())
	for (size_t n = 0; n < names.size(); ++n)
	  if (names[n] == t.identifier())
	    {
	      which = static_cast<int>(n);
	      break;
	    }
      if (which >= 0)
	{
	  // A parameter already solved is emitted as its concrete type, so
	  // the core type can become fully concrete (used to infer a
	  // parameter from its own constraint); an unsolved one becomes an
	  // inference marker to unify against a solved type.
	  if (solved != NULL && (size_t) which < solved->size()
	      && (*solved)[which] != NULL
	      && type_to_tokens((*solved)[which], subst, t.location(),
				&pkg_bindings))
	    ;
	  else
	    {
	      this->gogo_->infer_marker_type((size_t) which);
	      char buf[32];
	      snprintf(buf, sizeof buf, "$infermarker%d", which);
	      subst.push_back(Token::make_identifier_token(std::string(buf),
							   true, t.location()));
	    }
	}
      else
	subst.push_back(t);
    }
  // Quietly: this is constraint type inference.  A constraint whose core type
  // names a package not resolvable here (e.g. a method-set interface like
  // "internal.Request", or a package the current context can't disambiguate)
  // must yield NULL so inference falls back to the arguments, not emit a
  // spurious "undefined identifier".
  return this->parse_type_from_tokens(subst, &pkg_bindings,
				      /*issue_error=*/false);
}

// Generics: build the instance cache key for a set of type arguments.
// Each argument is resolved to a type and keyed by canonical type
// identity when possible, so that two spellings of the same type (for
// example "Box[int]" and the inferred instance name "Box$typeN") select
// the same instance; otherwise the argument's token spelling is used.

std::string
Parse::instance_key(const std::vector<std::vector<Token> >& type_args)
{
  std::string key;
  for (size_t i = 0; i < type_args.size(); ++i)
    {
      key += "$";
      for (size_t j = 0; j < type_args[i].size(); ++j)
	key += token_key_string(type_args[i][j]);
    }
  return key;
}

// Generics: check recorded constraint obligations.  Conservative: only
// enforces constraints that are a type-set of predeclared basic types
// (each optionally "~"), e.g. "int | float64" or "~int | ~string".  Any
// other constraint (a named interface, comparable, any, a structural or
// type-parameter-dependent constraint) is left unchecked, so this never
// rejects valid code.

// Generics: split a token sequence into groups separated by a top-level
// (bracket-depth-zero) occurrence of operator OP.

static std::vector<std::vector<Token> >
split_top_level(const std::vector<Token>& toks, Operator op)
{
  std::vector<std::vector<Token> > out;
  std::vector<Token> cur;
  int depth = 0;
  for (size_t i = 0; i < toks.size(); ++i)
    {
      const Token& t = toks[i];
      if (t.is_op(OPERATOR_LPAREN) || t.is_op(OPERATOR_LSQUARE)
	  || t.is_op(OPERATOR_LCURLY))
	++depth;
      else if (t.is_op(OPERATOR_RPAREN) || t.is_op(OPERATOR_RSQUARE)
	       || t.is_op(OPERATOR_RCURLY))
	--depth;
      if (depth == 0 && t.is_op(op))
	{
	  out.push_back(cur);
	  cur.clear();
	  continue;
	}
      cur.push_back(t);
    }
  out.push_back(cur);
  return out;
}

// Generics: flatten one constraint type-set element (an optional "~"
// followed by a type) into the term list TERMS, expanding a named type-set
// constraint if the element names one.  Returns false if the element is
// not an enforceable type-set element.

static bool
is_basic_type_name(const std::string& n)
{
  static const char* const basics[] = {
    "int", "int8", "int16", "int32", "int64",
    "uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
    "float32", "float64", "complex64", "complex128",
    "string", "bool", "byte", "rune", NULL
  };
  for (int b = 0; basics[b] != NULL; ++b)
    if (n == basics[b])
      return true;
  return false;
}

static bool
flatten_constraint_element(Gogo* gogo, const std::vector<Token>& elem,
			   std::vector<std::pair<bool, std::vector<Token> > >* terms,
			   int depth)
{
  if (depth > 16 || elem.empty())
    return false;
  size_t i = 0;
  bool approx = false;
  if (elem[0].is_op(OPERATOR_TILDE))
    {
      approx = true;
      i = 1;
    }
  if (i >= elem.size())
    return false;
  std::vector<Token> rest(elem.begin() + i, elem.end());

  if (rest.size() == 1 && rest[0].is_identifier())
    {
      const std::string& nm = rest[0].identifier();
      if (nm == "any" || nm == "comparable")
	return false;
      std::string packed =
	gogo->pack_hidden_name(nm, Lex::is_exported_name(nm));
      std::map<std::string, std::vector<std::vector<Token> > >::const_iterator
	it = named_constraint_type_sets.find(packed);
      if (it != named_constraint_type_sets.end())
	{
	  // A named type-set constraint; "~" is not allowed on it.
	  if (approx)
	    return false;
	  for (size_t k = 0; k < it->second.size(); ++k)
	    if (!flatten_constraint_element(gogo, it->second[k], terms,
					    depth + 1))
	      return false;
	  return true;
	}
      // A bare (non-"~") single name that is neither a basic predeclared
      // type nor a known type-set constraint is most likely a named
      // constraint interface we cannot expand here (for example one
      // imported from another package).  Do not enforce it, rather than
      // wrongly treating it as an exact-type term.
      if (!approx && !is_basic_type_name(nm))
	return false;
    }

  terms->push_back(std::make_pair(approx, rest));
  return true;
}

// Generics: flatten a whole constraint (the tokens between a type
// parameter name and the next "," or "]") into a list of type-set terms.
// Returns false if the constraint is not a pure, enforceable type-set
// (e.g. it contains methods, or names "any"/"comparable"), in which case
// the caller does not enforce it.

static bool
flatten_constraint(Gogo* gogo, const std::vector<Token>& c,
		   std::vector<std::pair<bool, std::vector<Token> > >* terms,
		   int depth)
{
  if (depth > 16 || c.empty())
    return false;

  // "interface { ... }" wrapper.
  if (c[0].is_keyword(KEYWORD_INTERFACE))
    {
      if (c.size() < 2 || !c[1].is_op(OPERATOR_LCURLY))
	return false;
      int d = 1;
      size_t j = 2;
      for (; j < c.size(); ++j)
	{
	  if (c[j].is_op(OPERATOR_LCURLY))
	    ++d;
	  else if (c[j].is_op(OPERATOR_RCURLY))
	    {
	      --d;
	      if (d == 0)
		break;
	    }
	}
      std::vector<Token> inner(c.begin() + 2, c.begin() + j);
      std::vector<std::vector<Token> > segs =
	split_top_level(inner, OPERATOR_SEMICOLON);
      for (size_t s = 0; s < segs.size(); ++s)
	{
	  if (segs[s].empty())
	    continue;
	  // A method element is "name ( ... )"; we cannot enforce method
	  // sets here, so skip the whole constraint.
	  if (segs[s].size() >= 2
	      && segs[s][0].is_identifier()
	      && segs[s][1].is_op(OPERATOR_LPAREN))
	    return false;
	  std::vector<std::vector<Token> > parts =
	    split_top_level(segs[s], OPERATOR_OR);
	  for (size_t p = 0; p < parts.size(); ++p)
	    if (!flatten_constraint_element(gogo, parts[p], terms, depth + 1))
	      return false;
	}
      return !terms->empty();
    }

  // Otherwise a "|"-separated list of elements (or a single element,
  // which may name another constraint).
  std::vector<std::vector<Token> > parts = split_top_level(c, OPERATOR_OR);
  for (size_t p = 0; p < parts.size(); ++p)
    if (!flatten_constraint_element(gogo, parts[p], terms, depth + 1))
      return false;
  return !terms->empty();
}

// Generics: rewrite a constraint into a self-contained form for export so
// that an importing package can enforce it without the defining package's
// constraint declarations.  A named type-set constraint all of whose
// elements are predeclared basic types (e.g. "Ordered") is expanded to
// the inline type-set "~int | ... | ~string"; anything else (a constraint
// mentioning package-specific named types, methods, comparable, or any)
// is left unchanged and is checked when the instance body is compiled.

static std::vector<Token>
expand_constraint_for_export(Gogo* gogo, const std::vector<Token>& c)
{
  std::vector<std::pair<bool, std::vector<Token> > > terms;
  if (!flatten_constraint(gogo, c, &terms, 0) || terms.empty())
    return c;
  for (size_t i = 0; i < terms.size(); ++i)
    if (!(terms[i].second.size() == 1
	  && terms[i].second[0].is_identifier()
	  && is_basic_type_name(terms[i].second[0].identifier())))
      return c;

  // If the constraint is already exactly this inline form, nothing to do;
  // either way, emitting the flattened form is correct and self-contained.
  Location loc = c.empty() ? Linemap::unknown_location() : c[0].location();
  std::vector<Token> out;
  for (size_t i = 0; i < terms.size(); ++i)
    {
      if (i > 0)
	out.push_back(Token::make_operator_token(OPERATOR_OR, loc));
      if (terms[i].first)
	out.push_back(Token::make_operator_token(OPERATOR_TILDE, loc));
      for (size_t j = 0; j < terms[i].second.size(); ++j)
	out.push_back(terms[i].second[j]);
    }
  return out;
}

// Generics: check recorded instantiations against their type-parameter
// constraints.  A constraint is enforced when it is a pure type-set
// (inline like "int | ~float64", or a named constraint interface such as
// "type Ordered interface { ~int | ~string }", possibly embedding other
// type-set constraints).  Constraints involving methods, "comparable" or
// "any", or any term that does not resolve to a concrete type, are left
// to be checked when the instance body is compiled, so this never rejects
// valid code.

void
Parse::check_generic_constraints()
{
  std::vector<Constraint_obligation*>& obs =
    this->gogo_->constraint_obligations();
  for (size_t oi = 0; oi < obs.size(); ++oi)
    {
      Constraint_obligation* o = obs[oi];
      // A constraint captured from an IMPORTED template names the defining
      // package's types by their bare names; enter that package's context so
      // they resolve (and are not reported as undefined types) while the
      // obligation's constraint tokens are re-parsed here, after parsing.
      bool ctx_pushed = false;
      if (o->defining_package != NULL)
	{
	  this->gogo_->push_instantiation_context();
	  this->gogo_->push_instantiation_package(o->defining_package);
	  ctx_pushed = true;
	}
      this->check_one_constraint(o);
      if (ctx_pushed)
	{
	  this->gogo_->pop_instantiation_package();
	  this->gogo_->pop_instantiation_context();
	}
    }
}

void
Parse::check_one_constraint(Constraint_obligation* o)
{
  {
      // A constraint that mentions one of the generic's own type
      // parameters (e.g. "lesser[T]" in "[T lesser[T]]") depends on them
      // and cannot be resolved to a concrete type here; resolving it would
      // instantiate a generic with an unbound parameter.  Leave it to be
      // checked when the instance body is compiled.
      bool depends_on_tparam = false;
      for (size_t ti = 0; ti < o->constraint.size() && !depends_on_tparam;
	   ++ti)
	{
	  if (!o->constraint[ti].is_identifier())
	    continue;
	  for (size_t pi = 0; pi < o->tparams.size(); ++pi)
	    if (o->constraint[ti].identifier() == o->tparams[pi])
	      {
		depends_on_tparam = true;
		break;
	      }
	}
      if (depends_on_tparam)
	return;

      Type* argType = this->resolve_constraint_type(o->arg, &o->pkg_aliases);
      if (argType == NULL || argType->is_error_type())
	return;

      // Pure type-set constraint (inline like "int | ~float64", or a
      // named constraint, possibly embedding other type-set constraints).
      std::vector<std::pair<bool, std::vector<Token> > > terms;
      if (flatten_constraint(this->gogo_, o->constraint, &terms, 0)
	  && !terms.empty())
	{
	  Type* argBase = argType->base();
	  if (argBase == NULL || argBase->is_error_type())
	    return;

	  // Resolve every term to a concrete type.  If any term is an
	  // interface (a method-set embedding) or fails to resolve, do not
	  // enforce this constraint.
	  std::vector<std::pair<bool, Type*> > resolved;
	  bool enforceable = true;
	  for (size_t t = 0; t < terms.size(); ++t)
	    {
	      Type* tt = this->resolve_constraint_type(terms[t].second,
							   &o->pkg_aliases);
	      if (tt == NULL || tt->is_error_type()
		  || tt->interface_type() != NULL)
		{
		  enforceable = false;
		  break;
		}
	      resolved.push_back(std::make_pair(terms[t].first, tt));
	    }
	  if (!enforceable)
	    return;

	  bool ok = false;
	  for (size_t t = 0; t < resolved.size() && !ok; ++t)
	    {
	      Type* tt = resolved[t].second;
	      if (resolved[t].first)
		{
		  // "~B": the argument's underlying type must be B.
		  if (Type::are_identical(argBase, tt->base(),
					  Type::COMPARE_ERRORS, NULL))
		    ok = true;
		}
	      else
		{
		  // "B": the argument must be exactly B.
		  if (Type::are_identical(argType, tt, Type::COMPARE_ERRORS,
					  NULL))
		    ok = true;
		}
	    }

	  if (!ok)
	    go_error_at(o->location,
			"type argument does not satisfy constraint of %qs",
			o->what.c_str());
	  return;
	}

      // The "comparable" constraint: the argument type must be comparable.
      if (o->constraint.size() == 1
	  && o->constraint[0].is_identifier()
	  && o->constraint[0].identifier() == "comparable")
	{
	  if (!argType->is_comparable())
	    go_error_at(o->location,
			"type argument does not satisfy constraint of %qs",
			o->what.c_str());
	  return;
	}

      // A method-set (interface) constraint: the argument must implement
      // the interface's methods.  Resolve the constraint to an interface
      // type -- a named interface, or an inline "interface { ... }".
      Type* ct = NULL;
      if (!o->constraint.empty())
	{
	  if (o->constraint[0].is_keyword(KEYWORD_INTERFACE))
	    ct = this->parse_type_from_tokens(o->constraint, &o->pkg_aliases);
	  else if (o->constraint.size() == 1 && o->constraint[0].is_identifier())
	    ct = this->resolve_constraint_type(o->constraint, &o->pkg_aliases);
	}
      if (ct != NULL && !ct->is_error_type()
	  && ct->interface_type() != NULL)
	{
	  Interface_type* it = ct->interface_type();
	  // The interface may not have been through the finalize-methods
	  // pass (e.g. one parsed here from inline constraint tokens), which
	  // is_empty/implements_interface require; finalize it first.
	  it->finalize_methods();
	  if (!it->is_empty())
	    {
	      // The argument may itself be an interface (e.g. one embedding the
	      // constraint interface).  implements_interface only consults the
	      // methods of named and struct types, so for an interface argument
	      // check directly that the constraint's method set is a subset of
	      // the argument's.
	      Interface_type* ait = argType->interface_type();
	      if (ait != NULL)
		{
		  ait->finalize_methods();
		  bool ok = true;
		  const Typed_identifier_list* ims = it->methods();
		  if (ims != NULL)
		    {
		      for (Typed_identifier_list::const_iterator pm = ims->begin();
			   pm != ims->end() && ok; ++pm)
			{
			  const Typed_identifier* am =
			    ait->find_method(pm->name());
			  if (am == NULL
			      || !Type::are_identical(am->type(), pm->type(),
						      Type::COMPARE_TAGS, NULL))
			    ok = false;
			}
		    }
		  if (!ok)
		    go_error_at(o->location,
				"type argument does not satisfy constraint of %qs",
				o->what.c_str());
		}
	      else
		{
		  std::string reason;
		  if (!it->implements_interface(argType, &reason))
		    go_error_at(o->location,
				"type argument does not satisfy constraint of %qs",
				o->what.c_str());
		}
	    }
	}
    }
}

// Generics: produce SUBSTITUTED from TMPL by replacing every identifier
// token that matches one of NAMES with the tokens of the corresponding
// type argument in ARGS.
//
// The replacement is textual, so it must skip identifiers that appear in
// positions where a type can never appear (otherwise an unrelated
// identifier that happens to be spelled like a type parameter would be
// rewritten).  Those positions are syntactically unambiguous:
//   * an identifier immediately after "." (a field/method selector, or a
//     qualified name) is never a type parameter;
//   * a struct field name (the identifier(s) before the field type).

static void
substitute_type_params(const std::vector<Token>& tmpl,
		       const std::vector<std::string>& names,
		       const std::vector<std::vector<Token> >& args,
		       std::vector<Token>& out)
{
  bool prev_dot = false;
  bool prev_struct_kw = false;
  // For each currently-open "{", whether it is a struct type body.
  std::vector<bool> struct_brace;
  // When directly inside a struct body, whether the next identifier is a
  // field name (rather than part of a field type).
  bool at_field_name = false;

  for (size_t i = 0; i < tmpl.size(); ++i)
    {
      const Token& t = tmpl[i];
      bool in_struct = !struct_brace.empty() && struct_brace.back();

      bool skip = prev_dot || (in_struct && at_field_name && t.is_identifier());

      int which = -1;
      if (t.is_identifier() && !skip)
	for (size_t k = 0; k < names.size() && k < args.size(); ++k)
	  if (names[k] == t.identifier())
	    {
	      which = (int) k;
	      break;
	    }

      if (which >= 0)
	{
	  const std::vector<Token>& rep = args[which];
	  // When the type parameter is used as a conversion "T(x)" and its
	  // type argument is a pointer type "*E", a plain textual replacement
	  // would yield "*E(x)", which parses as "*(E(x))".  Parenthesize the
	  // replacement -- "(*E)(x)" -- to preserve the intended meaning.  A
	  // parenthesized type is valid in type positions too.  The same applies
	  // to a method expression "T.M": with "*E" it would become "*E.M",
	  // parsed as "*(E.M)", so emit "(*E).M".
	  bool paren = (!rep.empty() && rep[0].is_op(OPERATOR_MULT)
			&& i + 1 < tmpl.size()
			&& (tmpl[i + 1].is_op(OPERATOR_LPAREN)
			    || tmpl[i + 1].is_op(OPERATOR_DOT)));
	  if (paren)
	    out.push_back(Token::make_operator_token(OPERATOR_LPAREN,
						     t.location()));
	  for (size_t j = 0; j < rep.size(); ++j)
	    out.push_back(rep[j]);
	  if (paren)
	    out.push_back(Token::make_operator_token(OPERATOR_RPAREN,
						     t.location()));
	}
      else
	out.push_back(t);

      // Update context for the next token.
      if (in_struct && at_field_name && t.is_identifier())
	{
	  // A field-name identifier stays in field-name mode only if a
	  // comma follows (a name list); otherwise the field type begins.
	  if (i + 1 >= tmpl.size() || !tmpl[i + 1].is_op(OPERATOR_COMMA))
	    at_field_name = false;
	}

      if (t.is_op(OPERATOR_LCURLY))
	{
	  struct_brace.push_back(prev_struct_kw);
	  at_field_name = prev_struct_kw;
	}
      else if (t.is_op(OPERATOR_RCURLY))
	{
	  if (!struct_brace.empty())
	    struct_brace.pop_back();
	  at_field_name = !struct_brace.empty() && struct_brace.back();
	}
      else if (t.is_op(OPERATOR_SEMICOLON) && in_struct)
	at_field_name = true;

      prev_dot = t.is_op(OPERATOR_DOT);
      prev_struct_kw = t.is_keyword(KEYWORD_STRUCT);
    }
}

// Generics: record a balanced bracket group into *OUT, including the
// opening and closing delimiters.  The current token must be an opener
// ("(", "[", or "{").  On return the current token is the one after the
// matching close delimiter.

void
Parse::capture_bracket_group(std::vector<Token>* out)
{
  int depth = 0;
  while (true)
    {
      const Token* t = this->peek_token();
      if (t->is_eof())
	break;
      out->push_back(*t);
      if (t->is_op(OPERATOR_LPAREN) || t->is_op(OPERATOR_LSQUARE)
	  || t->is_op(OPERATOR_LCURLY))
	++depth;
      else if (t->is_op(OPERATOR_RPAREN) || t->is_op(OPERATOR_RSQUARE)
	       || t->is_op(OPERATOR_RCURLY))
	{
	  --depth;
	  if (depth == 0)
	    {
	      this->advance_token();
	      break;
	    }
	}
      this->advance_token();
    }
}

// Generics: capture a method declaration whose receiver names a generic
// type, e.g. "func (s *Stack[T]) Push(x T) {...}".  RECV is the already
// captured receiver group, including its parentheses.  We record the
// type parameter names used in the receiver and the full method tokens,
// and attach the template to the generic type.

void
Parse::generic_method_decl(const std::vector<Token>& recv, Location location,
			   unsigned int pragmas)
{
  // Capture the rest of the method: name, signature, and body.  The
  // receiver tokens come first, and a blank "_" receiver type parameter is
  // renamed below within this copy so substitution stays positional.
  std::vector<Token> toks = recv;

  // Find the generic type name (the identifier just before the first
  // "[") and the receiver's type parameter names (identifiers at depth 1
  // inside "[...]").  A blank "_" receiver type parameter is given a unique
  // synthetic name (and its token rewritten in TOKS) so that two blanks
  // ("Foo[_, _]") do not collapse to one during positional substitution.
  std::string type_name;
  std::vector<std::string> recv_params;
  size_t lb = toks.size();
  for (size_t i = 0; i < toks.size(); ++i)
    if (toks[i].is_op(OPERATOR_LSQUARE))
      {
	lb = i;
	break;
      }
  for (size_t i = lb; i > 0; --i)
    if (toks[i - 1].is_identifier())
      {
	type_name = toks[i - 1].identifier();
	break;
      }
  {
    int depth = 0;
    for (size_t i = lb; i < toks.size(); ++i)
      {
	Token& t = toks[i];
	if (t.is_op(OPERATOR_LSQUARE))
	  ++depth;
	else if (t.is_op(OPERATOR_RSQUARE))
	  {
	    --depth;
	    if (depth == 0)
	      break;
	  }
	else if (depth == 1 && t.is_identifier())
	  {
	    if (t.identifier() == "_")
	      {
		static unsigned int blank_count;
		char buf[40];
		snprintf(buf, sizeof buf, "$blankrecvparam%u", blank_count);
		++blank_count;
		std::string syn(buf);
		t = Token::make_identifier_token(syn, false, t.location());
		recv_params.push_back(syn);
	      }
	    else
	      recv_params.push_back(t.identifier());
	  }
      }
  }
  const Token* nt = this->peek_token();
  if (nt->is_identifier())
    {
      toks.push_back(*nt);
      this->advance_token();
    }
  int depth = 0;
  bool body_started = false;
  while (true)
    {
      const Token* t = this->peek_token();
      if (t->is_eof())
	break;
      toks.push_back(*t);
      if (t->is_op(OPERATOR_LPAREN) || t->is_op(OPERATOR_LSQUARE)
	  || t->is_op(OPERATOR_LCURLY))
	{
	  // A "{" at depth 0 starts the body, unless it opens a "struct{...}"
	  // or "interface{...}" result type, whose "{" follows the keyword.
	  if (t->is_op(OPERATOR_LCURLY) && depth == 0
	      && !(toks.size() >= 2
		   && (toks[toks.size() - 2].is_keyword(KEYWORD_STRUCT)
		       || toks[toks.size() - 2].is_keyword(KEYWORD_INTERFACE))))
	    body_started = true;
	  ++depth;
	}
      else if (t->is_op(OPERATOR_RPAREN) || t->is_op(OPERATOR_RSQUARE)
	       || t->is_op(OPERATOR_RCURLY))
	{
	  --depth;
	  if (depth == 0 && body_started)
	    {
	      this->advance_token();
	      break;
	    }
	}
      this->advance_token();
    }
  toks.push_back(Token::make_eof_token(location));

  // The generic types are registered under their packed names.
  Generic_function_info* info =
    this->gogo_->lookup_generic_type(this->gogo_->pack_hidden_name(type_name,
								   Lex::is_exported_name(type_name)));
  // Record package usage and alias->pkgpath on the receiver type's template,
  // so an instance re-parse of the method body resolves qualifiers (e.g.
  // "internal.T") to the right package even amid same-named imports.
  this->note_token_package_usage(toks,
				 info != NULL ? &info->package_aliases() : NULL);
  if (info != NULL)
    {
      Generic_method_template mt;
      mt.recv_type_param_names = recv_params;
      mt.tokens = toks;
      info->methods().push_back(mt);

      // A generic type may be instantiated before its methods are declared
      // (e.g. "var _ I = &T[int]{}" written above the method declarations,
      // as in real code that asserts interface satisfaction).  Such earlier
      // instances were created with an empty method set; add this newly
      // declared method to each of them now so the method set is complete.
      std::vector<std::pair<Named_object*,
			    std::vector<std::vector<Token> > > >& insts =
	info->instance_list();
      for (size_t i = 0; i < insts.size(); ++i)
	{
	  // Skip transient marker-argument instances (see instantiate_generic_type).
	  bool marker_arg = false;
	  for (size_t a = 0; a < insts[i].second.size() && !marker_arg; ++a)
	    for (size_t j = 0; j < insts[i].second[a].size(); ++j)
	      if (insts[i].second[a][j].is_identifier()
		  && insts[i].second[a][j].identifier().compare(
		       0, 12, "$infermarker") == 0)
		{
		  marker_arg = true;
		  break;
		}
	  if (marker_arg)
	    continue;
	  std::vector<Token> msubst;
	  substitute_type_params(mt.tokens, mt.recv_type_param_names,
				 insts[i].second, msubst);
	  this->gogo_->push_instantiation_context();
	  bool imported = info->defining_package() != NULL;
	  if (imported)
	    this->gogo_->push_instantiation_package(info->defining_package());
	  this->instantiate_generic_method(msubst, location,
					   &info->package_aliases());
	  if (imported)
	    this->gogo_->pop_instantiation_package();
	  this->gogo_->pop_instantiation_context();
	  Named_type* nt = (insts[i].first->is_type()
			    ? insts[i].first->type_value()->named_type()
			    : NULL);
	  if (nt != NULL && this->gogo_->parsing_complete())
	    nt->finalize_methods(this->gogo_);
	}
    }
  // If INFO is NULL the generic type was not declared before its method;
  // that is an unsupported ordering, so we simply drop the method.

  if (pragmas != 0)
    go_warning_at(location, 0,
		  ("ignoring magic %<//go:...%> comment before "
		   "generic method"));
}

// Generics: re-parse a token-substituted method declaration (TOKS start
// at the receiver "(") as an ordinary method on a generic type instance.
// Returns the method's Named_object, or NULL on error.

Named_object*
Parse::instantiate_generic_method(std::vector<Token>& toks, Location location,
				  const std::map<std::string, std::string>* aliases)
{
  size_t defs_mark = this->gogo_->package_definitions_mark();

  Parse mp(this->lex_, this->gogo_);
  mp.set_replay_tokens(&toks);
  mp.set_replay_pkg_aliases(aliases);

  Typed_identifier* rec = mp.receiver();
  if (rec == NULL)
    return NULL;
  const Token* nt = mp.peek_token();
  if (!nt->is_identifier())
    return NULL;
  bool exported = nt->is_identifier_exported();
  // Pack the method name with the defining package (while instantiating an
  // imported template), matching how a selector packs the method name.
  std::string mname =
    this->gogo_->pack_hidden_name_for_field(nt->identifier(), exported);
  mp.advance_token();
  Function_type* fntype = mp.signature(rec, location);
  if (fntype == NULL)
    return NULL;
  Named_object* mno = this->gogo_->start_function(mname, fntype, true,
						  location);
  mp.block();
  this->gogo_->finish_function(location);

  if (this->gogo_->parsing_complete())
    {
      this->gogo_->resolve_global_names();
      this->gogo_->lower_builtin_calls_since(defs_mark);
    }

  return mno;
}

// Generics: capture a generic function template.  The current token is
// the "[" that introduces the type parameter list.  We record the type
// parameter names and the tokens of the signature and body, then
// register the template; no function is compiled here.

void
Parse::generic_function_decl(const std::string& name, bool is_exported,
			     Location location, unsigned int pragmas)
{
  Generic_function_info* info =
    new Generic_function_info(name, is_exported, location);

  this->type_parameter_names(&info->type_param_names(), &info->constraints());

  // A package qualifier may appear only in a type-parameter constraint
  // ("func F[T fmt.Stringer]..."); the constraint tokens are stored apart
  // from the captured body, so note their package usage too -- otherwise the
  // import is wrongly reported as unused.
  for (size_t ci = 0; ci < info->constraints().size(); ++ci)
    this->note_token_package_usage(info->constraints()[ci],
				   &info->package_aliases());

  // Capture the signature and body tokens.  The current token should be
  // "(".  We track bracket nesting and stop after the body's closing
  // "}" (the first "{" seen at depth 0 begins the body).
  std::vector<Token>& toks = info->tokens();
  int depth = 0;
  bool body_started = false;
  while (true)
    {
      const Token* t = this->peek_token();
      if (t->is_eof())
	break;
      toks.push_back(*t);
      if (t->is_op(OPERATOR_LPAREN) || t->is_op(OPERATOR_LSQUARE)
	  || t->is_op(OPERATOR_LCURLY))
	{
	  // A "{" at depth 0 starts the body, unless it opens a "struct{...}"
	  // or "interface{...}" result type, whose "{" follows the keyword.
	  if (t->is_op(OPERATOR_LCURLY) && depth == 0
	      && !(toks.size() >= 2
		   && (toks[toks.size() - 2].is_keyword(KEYWORD_STRUCT)
		       || toks[toks.size() - 2].is_keyword(KEYWORD_INTERFACE))))
	    body_started = true;
	  ++depth;
	}
      else if (t->is_op(OPERATOR_RPAREN) || t->is_op(OPERATOR_RSQUARE)
	       || t->is_op(OPERATOR_RCURLY))
	{
	  --depth;
	  if (depth == 0 && body_started)
	    {
	      this->advance_token();
	      break;
	    }
	}
      this->advance_token();
    }
  toks.push_back(Token::make_eof_token(location));
  this->note_token_package_usage(toks, &info->package_aliases());

  // A blank-named generic function ("func _[T any]() {}") can never be
  // referenced or instantiated, so do not register it or create a
  // placeholder (a function declaration named "_" is invalid).
  if (Gogo::is_sink_name(name))
    {
      if (pragmas != 0)
	go_warning_at(location, 0,
		      ("ignoring magic %<//go:...%> comment before "
		       "generic function"));
      return;
    }

  this->gogo_->add_generic_function(name, info);

  // Create a placeholder function declaration so that references to the
  // generic function by name resolve during parsing.  It is never
  // compiled or called directly; all uses are rewritten to instances.
  Function_type* placeholder =
    Type::make_function_type(NULL, NULL, NULL, location);
  this->gogo_->declare_function(name, placeholder, location);

  if (pragmas != 0)
    go_warning_at(location, 0,
		  ("ignoring magic %<//go:...%> comment before "
		   "generic function"));
}

// Generics: mark as used any imported package referenced by a "pkg.X"
// selector in a captured generic template, since the template body is
// compiled only when instantiated (possibly in another package) and so
// the normal use-tracking in operand() never sees it here.

void
Parse::note_token_package_usage(const std::vector<Token>& toks,
				std::map<std::string, std::string>* aliases)
{
  bool prev_dot = false;
  for (size_t i = 0; i + 1 < toks.size(); ++i)
    {
      bool was_dot = prev_dot;
      prev_dot = toks[i].is_op(OPERATOR_DOT);
      if (was_dot || !toks[i].is_identifier())
	continue;
      if (!toks[i + 1].is_op(OPERATOR_DOT))
	continue;
      std::string packed =
	this->gogo_->pack_hidden_name(toks[i].identifier(),
				      toks[i].is_identifier_exported());
      const std::string& alias = toks[i].identifier();
      // Resolve the qualifier to a pkgpath.  Order matters:
      //  (1) If we are re-parsing an instance, the current replay alias map is
      //      authoritative -- it carries the caller's binding for this alias,
      //      threaded down from the enclosing instantiation.  The file-scope
      //      imports have been cleared by clear_file_scope() by the time
      //      instantiation runs, so they are gone from package_->bindings().
      //  (2) Otherwise use the current package's OWN import bindings (never the
      //      ambiguous by-name reparse fallback, which could pick the wrong
      //      same-named package -- e.g. one of the many "internal" packages).
      std::string pkgpath;
      if (this->replay_pkg_aliases_ != NULL)
	{
	  std::map<std::string, std::string>::const_iterator a =
	    this->replay_pkg_aliases_->find(alias);
	  if (a != this->replay_pkg_aliases_->end())
	    pkgpath = a->second;
	}
      Named_object* no = NULL;
      if (pkgpath.empty())
	{
	  no = this->gogo_->lookup_pkg_binding(packed);
	  if (no != NULL && no->is_package())
	    pkgpath = no->package_value()->pkgpath();
	}
      if (!pkgpath.empty())
	{
	  if (no != NULL)
	    {
	      no->package_value()->note_usage(alias);
	      this->gogo_->add_generic_imported_package(no->package_value());
	    }
	  // Record this alias's pkgpath so an instance re-parse resolves the
	  // qualifier to the right package even when another imported package
	  // shares the same name.
	  if (aliases != NULL)
	    (*aliases)[alias] = pkgpath;
	}
    }
}

// Generics: whether TYPE still contains an inference marker anywhere in
// its structure (i.e. it is not yet fully concrete).  DEPTH bounds the
// recursion so that recursive generic types terminate.

static bool
type_has_infer_marker(Gogo* gogo, Type* type, int depth)
{
  if (type == NULL || depth > 24)
    return false;
  type = type->forwarded();
  if (gogo->infer_marker_index(type) >= 0)
    return true;
  if (type->points_to() != NULL)
    return type_has_infer_marker(gogo, type->points_to(), depth + 1);
  Array_type* at = type->array_type();
  if (at != NULL)
    return type_has_infer_marker(gogo, at->element_type(), depth + 1);
  Map_type* mt = type->map_type();
  if (mt != NULL)
    return (type_has_infer_marker(gogo, mt->key_type(), depth + 1)
	    || type_has_infer_marker(gogo, mt->val_type(), depth + 1));
  Channel_type* ct = type->channel_type();
  if (ct != NULL)
    return type_has_infer_marker(gogo, ct->element_type(), depth + 1);
  Struct_type* st = type->struct_type();
  if (st != NULL && st->fields() != NULL)
    {
      for (Struct_field_list::const_iterator p = st->fields()->begin();
	   p != st->fields()->end();
	   ++p)
	if (type_has_infer_marker(gogo, p->type(), depth + 1))
	  return true;
      return false;
    }
  Function_type* ft = type->function_type();
  if (ft != NULL)
    {
      if (ft->parameters() != NULL)
	for (Typed_identifier_list::const_iterator p = ft->parameters()->begin();
	     p != ft->parameters()->end();
	     ++p)
	  if (type_has_infer_marker(gogo, p->type(), depth + 1))
	    return true;
      if (ft->results() != NULL)
	for (Typed_identifier_list::const_iterator p = ft->results()->begin();
	     p != ft->results()->end();
	     ++p)
	  if (type_has_infer_marker(gogo, p->type(), depth + 1))
	    return true;
      return false;
    }
  return false;
}

// Generics: structurally unify a parameter type PT (which may contain
// inference marker types) against an argument type AT, recording the
// solved type for each marker in SOLVED.  Handles the common forms:
// direct T, []T, *T, map[K]V, chan T, func(...) ..., and the fields of a
// (named or literal) struct, which covers a parameter whose type is an
// instantiation of another generic type, e.g. func F[T any](b Box[T]).
// DEPTH bounds the recursion so that recursive generic types terminate.

static void
unify_marker(Gogo* gogo, Type* pt, Type* at, std::vector<Type*>& solved,
	     int depth, bool from_untyped = false,
	     std::vector<bool>* solved_untyped = NULL)
{
  if (pt == NULL || at == NULL || depth > 24)
    return;
  pt = pt->forwarded();
  at = at->forwarded();

  int mi = gogo->infer_marker_index(pt);
  if (mi >= 0)
    {
      Type* a = at;
      if (a->is_abstract())
	a = a->make_non_abstract_type();
      if ((size_t) mi < solved.size())
	{
	  if (solved[mi] == NULL)
	    {
	      solved[mi] = a;
	      if (solved_untyped != NULL)
		(*solved_untyped)[mi] = from_untyped;
	    }
	  else if (solved_untyped != NULL && (*solved_untyped)[mi]
		   && !from_untyped)
	    {
	      // The marker was tentatively solved from an untyped constant
	      // argument (e.g. the identity 0 for a parameter of type T); a
	      // typed argument is more authoritative, so override it.
	      solved[mi] = a;
	      (*solved_untyped)[mi] = false;
	    }
	}
      return;
    }

  // If the two types are the very same object, there is no marker to solve by
  // descending (any marker in PT is the same marker in AT).  Stop: descending
  // anyway would needlessly walk -- and, for a self-referential interface
  // method graph, exponentially explode -- a structure with nothing to unify.
  if (pt == at)
    return;

  // Two instances of the same generic type (e.g. getter[T] and getter[int],
  // including generic interface and other non-struct instances): unify their
  // corresponding type arguments.  This covers forms that structural
  // decomposition below does not reach, such as a generic interface.
  Named_type* pnt = pt->named_type();
  Named_type* ant = at->named_type();
  if (pnt != NULL && ant != NULL
      && !pnt->generic_type_args().empty()
      && !pnt->generic_base_name().empty()
      && pnt->generic_base_name() == ant->generic_base_name()
      && pnt->generic_type_args().size() == ant->generic_type_args().size())
    {
      const std::vector<Type*>& pa = pnt->generic_type_args();
      const std::vector<Type*>& aa = ant->generic_type_args();
      for (size_t i = 0; i < pa.size(); ++i)
	unify_marker(gogo, pa[i], aa[i], solved, depth + 1, from_untyped,
		     solved_untyped);
      return;
    }

  if (pt->points_to() != NULL && at->points_to() != NULL)
    {
      unify_marker(gogo, pt->points_to(), at->points_to(), solved, depth + 1,
		   from_untyped, solved_untyped);
      return;
    }

  Array_type* pat = pt->array_type();
  Array_type* aat = at->array_type();
  if (pat != NULL && aat != NULL
      && pat->length() == NULL && aat->length() == NULL)
    {
      unify_marker(gogo, pat->element_type(), aat->element_type(), solved,
		   depth + 1, from_untyped, solved_untyped);
      return;
    }

  Map_type* pmt = pt->map_type();
  Map_type* amt = at->map_type();
  if (pmt != NULL && amt != NULL)
    {
      unify_marker(gogo, pmt->key_type(), amt->key_type(), solved, depth + 1,
		   from_untyped, solved_untyped);
      unify_marker(gogo, pmt->val_type(), amt->val_type(), solved, depth + 1,
		   from_untyped, solved_untyped);
      return;
    }

  Channel_type* pct = pt->channel_type();
  Channel_type* act = at->channel_type();
  if (pct != NULL && act != NULL)
    {
      unify_marker(gogo, pct->element_type(), act->element_type(), solved,
		   depth + 1, from_untyped, solved_untyped);
      return;
    }

  // Two interface types (e.g. two instantiations of a generic interface such
  // as Sender[T] vs Sender[Request]): unify corresponding methods by name.
  // interface_type() sees through a named type, so this also covers a generic
  // interface instance imported from another package (whose recorded
  // generic_type_args()/generic_base_name() are absent, so the instance branch
  // above does not fire) unified against a locally built marker instance.
  Interface_type* pit = pt->interface_type();
  Interface_type* ait = at->interface_type();
  if (pit != NULL && ait != NULL)
    {
      pit->finalize_methods();
      ait->finalize_methods();
      const Typed_identifier_list* pm = pit->methods();
      const Typed_identifier_list* am = ait->methods();
      if (pm != NULL && am != NULL)
	{
	  for (Typed_identifier_list::const_iterator i1 = pm->begin();
	       i1 != pm->end();
	       ++i1)
	    {
	      const Typed_identifier* m2 = ait->find_method(i1->name());
	      if (m2 != NULL)
		unify_marker(gogo, i1->type(), m2->type(), solved, depth + 1,
			     from_untyped, solved_untyped);
	    }
	}
      return;
    }

  Function_type* pft = pt->function_type();
  Function_type* aft = at->function_type();
  if (pft != NULL && aft != NULL)
    {
      const Typed_identifier_list* pp = pft->parameters();
      const Typed_identifier_list* ap = aft->parameters();
      if (pp != NULL && ap != NULL)
	{
	  Typed_identifier_list::const_iterator i1 = pp->begin();
	  Typed_identifier_list::const_iterator i2 = ap->begin();
	  for (; i1 != pp->end() && i2 != ap->end(); ++i1, ++i2)
	    unify_marker(gogo, i1->type(), i2->type(), solved, depth + 1,
			 from_untyped, solved_untyped);
	}
      const Typed_identifier_list* pr = pft->results();
      const Typed_identifier_list* ar = aft->results();
      if (pr != NULL && ar != NULL)
	{
	  Typed_identifier_list::const_iterator i1 = pr->begin();
	  Typed_identifier_list::const_iterator i2 = ar->begin();
	  for (; i1 != pr->end() && i2 != ar->end(); ++i1, ++i2)
	    unify_marker(gogo, i1->type(), i2->type(), solved, depth + 1,
			 from_untyped, solved_untyped);
	}
      return;
    }

  // Two struct types (typically two instantiations of the same generic
  // type, e.g. Box[T] vs Box[string]): unify corresponding fields.  We
  // match by field name and position to avoid unifying coincidentally
  // similar but unrelated structs.
  Struct_type* pst = pt->struct_type();
  Struct_type* ast = at->struct_type();
  if (pst != NULL && ast != NULL)
    {
      const Struct_field_list* pf = pst->fields();
      const Struct_field_list* af = ast->fields();
      if (pf != NULL && af != NULL && pf->size() == af->size())
	{
	  Struct_field_list::const_iterator i1 = pf->begin();
	  Struct_field_list::const_iterator i2 = af->begin();
	  for (; i1 != pf->end() && i2 != af->end(); ++i1, ++i2)
	    {
	      if (i1->field_name() != i2->field_name())
		return;
	      unify_marker(gogo, i1->type(), i2->type(), solved, depth + 1,
			   from_untyped, solved_untyped);
	    }
	}
    }
}

// Generics: emit source tokens that name the type T, for use as a type
// argument in an instantiation.  Returns false if T cannot be expressed
// (in which case inference fails and the user must give explicit type
// arguments).

static bool
type_to_tokens(Type* t, std::vector<Token>& out, Location loc,
	       std::map<std::string, std::string>* pkg_bindings)
{
  t = t->forwarded();
  if (t->is_abstract())
    t = t->make_non_abstract_type();

  // A named type (including a predeclared type like "int" or "error", a
  // user type, or a generic instance) is emitted by name -- checked first,
  // because points_to/array_type/etc. see through named types and would
  // otherwise emit the underlying type and lose the name.
  Named_type* nt = t->named_type();
  if (nt != NULL)
    {
      // A FUNCTION-LOCAL named type (including a local generic instance) is
      // emitted as its stable synthetic "$localtypeN" name, which maps back to
      // this exact type object during replay -- its bare source name would be
      // out of scope where the enclosing generic is instantiated.  Checked
      // before the generic-instance spelling so a local generic instance
      // "U[int]" emits "$localtypeN" for the exact instance (preserving
      // identity) rather than its "U[int]" spelling.  Skip an alias: use its
      // aliased target's identity instead (below / via forwarded()).
      {
	unsigned int fidx;
	if (!nt->is_alias() && nt->in_function(&fidx) != NULL)
	  {
	    Named_object* lno = nt->named_object();
	    out.push_back(Token::make_identifier_token(
	      replay_name_for_local_type(lno), true, loc));
	    return true;
	  }
      }
      // A generic instance ("Box$type0") is emitted by its canonical
      // spelling ("Box[arg, ...]") so it hashes to the same instance as
      // the type written out directly.
      std::map<const Named_object*, std::vector<Token> >::const_iterator sp =
	generic_instance_spelling.find(nt->named_object());
      if (sp != generic_instance_spelling.end())
	{
	  for (size_t i = 0; i < sp->second.size(); ++i)
	    out.push_back(sp->second[i]);
	  return true;
	}

      const std::string& n = nt->name();
      bool hidden = Gogo::is_hidden_name(n);
      std::string src = hidden ? Gogo::unpack_hidden_name(n) : n;
      // The token's "exported" flag must match how the name is normally
      // tokenized (e.g. "int" is not exported), so that name packing and
      // lookup are consistent.
      bool exported = hidden ? false : Lex::is_exported_name(src);
      // A type defined in another package must be emitted qualified
      // ("pkg.Name"); otherwise the bare name is undefined when the
      // instantiation is re-parsed (e.g. an inferred type argument of type
      // "unit.Unit").  Predeclared and current-package types have no package.
      Named_object* tno = nt->named_object();
      const Package* tpkg = (tno != NULL ? tno->package() : NULL);
      if (tpkg != NULL && tpkg->has_package_name() && exported)
	{
	  out.push_back(
	    Token::make_identifier_token(tpkg->package_name(), false, loc));
	  out.push_back(Token::make_operator_token(OPERATOR_DOT, loc));
	  // Record the package-name -> pkgpath binding so the instantiation
	  // replay can resolve this qualifier to the exact package the type
	  // argument came from.  Emitting only the package *name* is ambiguous
	  // when two imported packages share a name (e.g. a package
	  // "resolver" and an aliased "internal/resolver" also named
	  // "resolver"); the by-name lookup during replay could otherwise pick
	  // the wrong one and report the type as undefined.
	  if (pkg_bindings != NULL)
	    pkg_bindings->insert(std::make_pair(tpkg->package_name(),
						tpkg->pkgpath()));
	}
      out.push_back(Token::make_identifier_token(src, exported, loc));
      return true;
    }

  if (t->points_to() != NULL)
    {
      out.push_back(Token::make_operator_token(OPERATOR_MULT, loc));
      return type_to_tokens(t->points_to(), out, loc, pkg_bindings);
    }

  Array_type* at = t->array_type();
  if (at != NULL && at->length() == NULL)
    {
      out.push_back(Token::make_operator_token(OPERATOR_LSQUARE, loc));
      out.push_back(Token::make_operator_token(OPERATOR_RSQUARE, loc));
      return type_to_tokens(at->element_type(), out, loc, pkg_bindings);
    }
  if (at != NULL && at->length() != NULL)
    {
      // A fixed-size array "[N]E": emit the (constant) length.
      Numeric_constant nc;
      mpz_t val;
      if (at->length()->numeric_constant_value(&nc) && nc.to_int(&val))
	{
	  out.push_back(Token::make_operator_token(OPERATOR_LSQUARE, loc));
	  out.push_back(Token::make_integer_token(val, loc));
	  out.push_back(Token::make_operator_token(OPERATOR_RSQUARE, loc));
	  mpz_clear(val);
	  return type_to_tokens(at->element_type(), out, loc, pkg_bindings);
	}
      return false;
    }

  Map_type* mt = t->map_type();
  if (mt != NULL)
    {
      out.push_back(Token::make_keyword_token(KEYWORD_MAP, loc));
      out.push_back(Token::make_operator_token(OPERATOR_LSQUARE, loc));
      if (!type_to_tokens(mt->key_type(), out, loc, pkg_bindings))
	return false;
      out.push_back(Token::make_operator_token(OPERATOR_RSQUARE, loc));
      return type_to_tokens(mt->val_type(), out, loc, pkg_bindings);
    }

  Channel_type* ct = t->channel_type();
  if (ct != NULL)
    {
      // "chan E", "chan<- E" (send only) or "<-chan E" (receive only).
      if (!ct->may_send())
	out.push_back(Token::make_operator_token(OPERATOR_CHANOP, loc));
      out.push_back(Token::make_keyword_token(KEYWORD_CHAN, loc));
      if (ct->may_send() && !ct->may_receive())
	out.push_back(Token::make_operator_token(OPERATOR_CHANOP, loc));
      return type_to_tokens(ct->element_type(), out, loc, pkg_bindings);
    }

  // A struct type "struct { name type; ...; EmbeddedType; ... }", as can
  // arise as an inferred type argument (e.g. the value type of a map).
  Struct_type* st = t->struct_type();
  if (st != NULL)
    {
      out.push_back(Token::make_keyword_token(KEYWORD_STRUCT, loc));
      out.push_back(Token::make_operator_token(OPERATOR_LCURLY, loc));
      const Struct_field_list* fields = st->fields();
      if (fields != NULL)
	{
	  bool first = true;
	  for (Struct_field_list::const_iterator p = fields->begin();
	       p != fields->end();
	       ++p)
	    {
	      if (!first)
		out.push_back(Token::make_operator_token(OPERATOR_SEMICOLON,
							 loc));
	      first = false;
	      if (!p->is_anonymous())
		{
		  const std::string& fn = p->field_name();
		  // Preserve an unexported field's existing hidden spelling so a
		  // replayed generic instantiation keeps the field's origin package
		  // path instead of re-packing it with the instantiating package's
		  // path.  Exported fields still round-trip as plain identifiers.
		  std::string src = (Gogo::is_hidden_name(fn)
				     ? fn
				     : Gogo::unpack_hidden_name(fn));
		  out.push_back(Token::make_identifier_token(
		    src, Lex::is_exported_name(src), loc));
		}
	      if (!type_to_tokens(p->type(), out, loc, pkg_bindings))
		return false;
	      if (p->has_tag())
		out.push_back(Token::make_string_token(p->tag(), loc));
	    }
	}
      out.push_back(Token::make_operator_token(OPERATOR_RCURLY, loc));
      return true;
    }

  // An empty interface "interface{}" (or the predeclared "any"); a
  // non-empty interface cannot be reconstructed reliably from tokens, so
  // only the empty case is emitted.
  Interface_type* it = t->interface_type();
  if (it != NULL)
    {
      it->finalize_methods();
      if (it->methods() != NULL && !it->methods()->empty())
	return false;
      out.push_back(Token::make_keyword_token(KEYWORD_INTERFACE, loc));
      out.push_back(Token::make_operator_token(OPERATOR_LCURLY, loc));
      out.push_back(Token::make_operator_token(OPERATOR_RCURLY, loc));
      return true;
    }

  // A function type "func(params) results", as can arise as an inferred type
  // argument (e.g. the value type of a map[K]func(...), inferred for
  // maps.Clone / maps.Copy in net/http).  Parameter and result names are
  // omitted (they are not part of the type); a variadic final parameter is
  // written "...E".
  Function_type* ft = t->function_type();
  if (ft != NULL && !ft->is_method())
    {
      out.push_back(Token::make_keyword_token(KEYWORD_FUNC, loc));
      out.push_back(Token::make_operator_token(OPERATOR_LPAREN, loc));
      const Typed_identifier_list* params = ft->parameters();
      if (params != NULL)
	{
	  bool first = true;
	  for (Typed_identifier_list::const_iterator p = params->begin();
	       p != params->end();
	       ++p)
	    {
	      if (!first)
		out.push_back(Token::make_operator_token(OPERATOR_COMMA, loc));
	      first = false;
	      Type* pt = p->type();
	      if (ft->is_varargs() && p + 1 == params->end()
		  && pt->array_type() != NULL
		  && pt->array_type()->length() == NULL)
		{
		  out.push_back(Token::make_operator_token(OPERATOR_ELLIPSIS,
							   loc));
		  if (!type_to_tokens(pt->array_type()->element_type(), out,
				      loc, pkg_bindings))
		    return false;
		}
	      else if (!type_to_tokens(pt, out, loc, pkg_bindings))
		return false;
	    }
	}
      out.push_back(Token::make_operator_token(OPERATOR_RPAREN, loc));
      const Typed_identifier_list* results = ft->results();
      if (results != NULL && !results->empty())
	{
	  bool multi = results->size() > 1;
	  if (multi)
	    out.push_back(Token::make_operator_token(OPERATOR_LPAREN, loc));
	  bool first = true;
	  for (Typed_identifier_list::const_iterator p = results->begin();
	       p != results->end();
	       ++p)
	    {
	      if (!first)
		out.push_back(Token::make_operator_token(OPERATOR_COMMA, loc));
	      first = false;
	      if (!type_to_tokens(p->type(), out, loc, pkg_bindings))
		return false;
	    }
	  if (multi)
	    out.push_back(Token::make_operator_token(OPERATOR_RPAREN, loc));
	}
      return true;
    }

  return false;
}

// Generics: infer type arguments for a call to a generic function from
// the argument expression types, then instantiate.

Named_object*
Parse::instantiate_generic_with_inference(Generic_function_info* info,
					  Expression_list* args,
					  Location location,
					  const std::vector<std::vector<Token> >* partial,
					  bool call_is_spread,
					  bool quiet,
					  const std::map<std::string, std::string>*
					    extra_pkg_aliases)
{
  size_t nparams = info->type_param_names().size();

  // Build (once) the signature with marker types substituted for the
  // type parameters, so we can unify it against the argument types.
  Function_type* gsig = info->marker_signature();
  if (gsig == NULL)
    {
      // Replace each type-parameter name with its inference marker.  Use
      // substitute_type_params (rather than a plain replace-all) so that an
      // identifier in a non-type position -- in particular a struct field
      // name that happens to match a type parameter, as in
      // "struct{ Body Body }" -- is not rewritten, leaving field names
      // intact for unification.
      std::vector<std::vector<Token> > marker_args(nparams);
      for (size_t k = 0; k < nparams; ++k)
	{
	  this->gogo_->infer_marker_type(k);
	  char buf[32];
	  snprintf(buf, sizeof buf, "$infermarker%zu", k);
	  marker_args[k].push_back(
	    Token::make_identifier_token(std::string(buf), true, location));
	}
      std::vector<Token> subst;
      substitute_type_params(info->tokens(), info->type_param_names(),
			     marker_args, subst);

      this->gogo_->push_instantiation_context();
      bool imported_sig = info->defining_package() != NULL;
      if (imported_sig)
	this->gogo_->push_instantiation_package(info->defining_package());
      Parse sp(this->lex_, this->gogo_);
      sp.set_replay_tokens(&subst);
      sp.set_replay_pkg_aliases(&info->package_aliases());
      gsig = sp.signature(NULL, location);
      if (imported_sig)
	this->gogo_->pop_instantiation_package();
      this->gogo_->pop_instantiation_context();
      info->set_marker_signature(gsig);
    }

  // Unify parameter types against argument types to solve each marker.
  // Argument types are read from throwaway copies so that the real
  // argument expressions are left undetermined for the normal pass.
  std::vector<Type*> solved(nparams, (Type*) NULL);
  // Tracks, per solved marker, whether it was solved only from an untyped
  // constant argument; such a solution is overridden by a later typed
  // argument so that, e.g., the untyped 0 in Reduce(s, 0, func(float64,...))
  // does not fix the type parameter to int.
  std::vector<bool> solved_untyped(nparams, false);
  const Typed_identifier_list* params = (gsig == NULL
					 ? NULL
					 : gsig->parameters());
  bool is_varargs = (gsig != NULL && gsig->is_varargs());

  // A single argument that is a call returning multiple values supplies
  // all the parameters, e.g. try(f()) where f returns (T, error).  Unify
  // each result type against the corresponding parameter.
  bool handled_multi = false;
  if (args != NULL && args->size() == 1 && params != NULL
      && params->size() > 1 && !is_varargs)
    {
      Expression* a0 = args->front()->copy();
      a0->determine_type_no_context(this->gogo_);
      Call_expression* ce = a0->call_expression();
      if (ce != NULL && ce->result_count() == params->size()
	  && ce->fn() != NULL && ce->fn()->type() != NULL
	  && ce->fn()->type()->function_type() != NULL
	  && ce->fn()->type()->function_type()->results() != NULL)
	{
	  const Typed_identifier_list* res =
	    ce->fn()->type()->function_type()->results();
	  Typed_identifier_list::const_iterator pp = params->begin();
	  Typed_identifier_list::const_iterator rp = res->begin();
	  for (; pp != params->end() && rp != res->end(); ++pp, ++rp)
	    if (rp->type() != NULL && !rp->type()->is_error_type())
	      unify_marker(this->gogo_, pp->type(), rp->type(), solved, 0);
	  handled_multi = true;
	}
    }

  if (!handled_multi && params != NULL && args != NULL)
    {
      size_t nparam = params->size();
      Typed_identifier_list::const_iterator pp = params->begin();
      size_t pi = 0;
      for (Expression_list::const_iterator pa = args->begin();
	   pa != args->end();
	   ++pa)
	{
	  if (pi >= nparam)
	    break;
	  Expression* copy = (*pa)->copy();
	  // Whether this argument is an untyped constant (e.g. the literal 0,
	  // or a named untyped constant such as "Small" or "math.MaxUint32").
	  // is_untyped resolves through named-constant and unknown references
	  // to report the underlying abstract type, and works before the
	  // expression is given a default type.
	  Type* utype = NULL;
	  bool untyped = copy->is_untyped(&utype);
	  Type* at;
	  if (untyped)
	    {
	      // Do NOT call determine_type_no_context on an untyped constant:
	      // for a named constant, copy() shares the underlying
	      // Named_constant, so determining it here would permanently cache
	      // its type as the default (e.g. int), poisoning the real argument
	      // so it could no longer take the parameter's type at the call.
	      // Unify against the constant's default type, exactly as a literal
	      // would; the solved_untyped bookkeeping lets a later typed
	      // argument override this.
	      at = (utype != NULL && utype->is_abstract()
		    ? utype->make_non_abstract_type()
		    : utype);
	    }
	  else
	    {
	      copy->determine_type_no_context(this->gogo_);
	      at = copy->type();
	    }

	  bool last_is_varargs = (is_varargs && pi + 1 == nparam);
	  // An untyped nil argument carries no type information and must not
	  // be unified against the parameter (which would wrongly solve a
	  // type parameter to the nil type and block inference from the other
	  // arguments).
	  if (at != NULL && !at->is_error_type() && !at->is_void_type()
	      && !at->is_nil_type())
	    {
	      Type* pt = pp->type();
	      if (last_is_varargs && call_is_spread)
		{
		  // A spread call "f(s...)" passes the slice directly, so the
		  // argument type unifies with the whole "[]E" parameter.
		  unify_marker(this->gogo_, pt, at, solved, 0, untyped,
			       &solved_untyped);
		}
	      else if (last_is_varargs)
		{
		  // A variadic parameter "xs ...E" has type "[]E"; unify the
		  // element type E with each trailing argument.
		  Array_type* a = pt->array_type();
		  Type* elem = (a != NULL ? a->element_type() : pt);
		  unify_marker(this->gogo_, elem, at, solved, 0, untyped,
			       &solved_untyped);
		}
	      else
		unify_marker(this->gogo_, pt, at, solved, 0, untyped,
			     &solved_untyped);
	    }

	  // Advance to the next parameter, but stay on a trailing variadic
	  // parameter so it can absorb the remaining arguments.
	  if (!last_is_varargs)
	    {
	      ++pp;
	      ++pi;
	    }
	}
    }

  // Seed the solved set with any explicitly-supplied (partial) type
  // arguments, so constraint type inference can use them to solve the
  // remaining parameters.
  if (partial != NULL)
    for (size_t i = 0; i < nparams && i < partial->size(); ++i)
      if (solved[i] == NULL)
	{
	  // Resolve the explicit argument's package qualifiers via the
	  // bindings captured at parse time: the file-scope imports are gone
	  // by now, so a qualifier like "tpm2" in "tpm2.TPMTPublic" would
	  // otherwise fail to resolve.
	  Type* pt = this->parse_type_from_tokens((*partial)[i],
						  extra_pkg_aliases);
	  if (pt != NULL && !pt->is_error_type())
	    solved[i] = pt;
	}

  // Alias any function-local types appearing in the already-solved arguments
  // before constraint type inference derives constraint cores from them:
  // deriving a core spells its solved parameters via type_to_tokens, and a
  // bare function-local name emitted there would create an orphan package-
  // scope forward declaration that later warns "use of undefined type".
  // Registering the aliases first makes the core spell the alias instead.
  for (size_t i = 0; i < nparams; ++i)
    if (solved[i] != NULL)
      this->register_local_type_aliases(solved[i], location, 0);

  // Constraint type inference: a type parameter may appear only in the
  // constraint of another parameter (e.g. K and V in
  // "[M ~map[K]V, K comparable, V any]"), or a parameter may be determined
  // by its own constraint's core type (e.g. "PT Setter[T]" where
  // "Setter[B] interface{ Set(string); *B }" gives PT = *T).  Iterate to a
  // fixpoint, since one constraint may depend on a parameter solved by
  // another.
  {
    std::vector<std::vector<Token> >& cons = info->constraints();
    // A constraint captured from an IMPORTED template refers to the defining
    // package's types by bare name (e.g. the embedded "Unmarshallable" in
    // go-tpm's "P interface{ *T; Unmarshallable }").  Enter that package's
    // context so constraint_core_type_with_markers can resolve such a name to
    // its interface type and correctly skip it (leaving "*T" as the core type
    // that determines P), rather than treating it as a second structural term
    // and giving up on inference.
    bool cons_ctx = false;
    if (info->defining_package() != NULL)
      {
	this->gogo_->push_instantiation_context();
	this->gogo_->push_instantiation_package(info->defining_package());
	cons_ctx = true;
      }
    bool progress = true;
    while (progress)
      {
	progress = false;
	for (size_t i = 0; i < nparams && i < cons.size(); ++i)
	  {
	    if (cons[i].empty())
	      continue;

	    if (solved[i] != NULL)
	      {
		// Use this solved parameter's constraint core type to solve
		// the other parameters appearing in it.
		std::vector<Type*> core_solved(solved);
		for (size_t s = 0; s < core_solved.size(); ++s)
		  if (s != i && solved_untyped[s])
		    core_solved[s] = NULL;
		Type* core =
		  this->constraint_core_type_with_markers(
		    cons[i], info->type_param_names(), &core_solved,
		    &info->package_aliases());
		if (core == NULL || core->is_error_type())
		  continue;
		size_t before = 0;
		for (size_t s = 0; s < nparams; ++s)
		  if (solved[s] != NULL)
		    ++before;
		// A constraint-derived solution from an already-solved typed
		// parameter should override an earlier tentative solution that
		// came only from an untyped constant argument.
		unify_marker(this->gogo_, core, solved[i], solved, 0,
			     /*from_untyped=*/false, &solved_untyped);
		size_t after = 0;
		for (size_t s = 0; s < nparams; ++s)
		  if (solved[s] != NULL)
		    ++after;
		if (after > before)
		  progress = true;
	      }
	    else
	      {
		// Solve this parameter from its own constraint's core type if
		// that core type is now fully concrete (all other parameters
		// it mentions are solved).
		Type* core =
		  this->constraint_core_type_with_markers(
		    cons[i], info->type_param_names(), &solved,
		    &info->package_aliases());
		if (core == NULL || core->is_error_type())
		  continue;
		if (!type_has_infer_marker(this->gogo_, core, 0))
		  {
		    solved[i] = core;
		    progress = true;
		  }
	      }
	  }
      }
    if (cons_ctx)
      {
	this->gogo_->pop_instantiation_package();
	this->gogo_->pop_instantiation_context();
      }
  }

  // Convert each solved type to tokens to use as a type argument.  Type
  // parameters supplied explicitly in a partial instantiation use those
  // tokens directly; the rest come from inference.
  std::vector<std::vector<Token> > type_args(nparams);
  // Package-name -> pkgpath bindings for the (cross-package) types emitted as
  // type arguments, so the instance re-parse resolves each qualifier to the
  // exact package it came from even when another same-named package is in
  // scope.
  std::map<std::string, std::string> pkg_bindings;
  // The partial arguments' qualifier bindings were captured at parse time
  // (before file-scope imports were cleared); make them available to the
  // instance re-parse so a qualifier like "tpm2" still resolves.
  if (extra_pkg_aliases != NULL)
    pkg_bindings.insert(extra_pkg_aliases->begin(), extra_pkg_aliases->end());
  for (size_t i = 0; i < nparams; ++i)
    {
      if (partial != NULL && i < partial->size())
	{
	  type_args[i] = (*partial)[i];
	  // An explicitly-supplied type argument (e.g. "Fn[internal.Request]")
	  // is used as source tokens directly, bypassing type_to_tokens; record
	  // its package-qualifier bindings from the caller's imports so the
	  // instance re-parse resolves each qualifier to the exact package.
	  this->note_token_package_usage((*partial)[i], &pkg_bindings);
	  continue;
	}
      // A solved type that is a function-local named type is not visible at
      // package scope where the instance is re-parsed; emit a package-scope
      // alias name for it instead.
      Named_type* snt = (solved[i] != NULL
			 ? solved[i]->forwarded()->named_type()
			 : NULL);
      if (snt != NULL)
	{
	  unsigned int idx;
	  if (snt->in_function(&idx) != NULL)
	    {
	      std::string syn =
		this->package_alias_for_local_type(solved[i], location);
	      type_args[i].clear();
	      type_args[i].push_back(
		Token::make_identifier_token(syn, false, location));
	      continue;
	    }
	}
      // The solved type may instead be a composite (e.g. "[]le") nesting a
      // function-local named type; alias each embedded local type so
      // type_to_tokens emits the alias where the local type appears.
      if (solved[i] != NULL)
	this->register_local_type_aliases(solved[i], location, 0);
      // If the solved type still contains an inference marker, the argument
      // it came from was itself a not-yet-resolved instance (e.g. an outer
      // template's parameter standing in as "$infermarkerK" while building a
      // marker signature).  Inference genuinely did not resolve this
      // parameter, so fail rather than emitting "$infermarkerK" as a type
      // argument and creating a broken instance.
      if (solved[i] != NULL
	  && type_has_infer_marker(this->gogo_, solved[i], 0))
	{
	  if (!quiet)
	    go_error_at(location,
			("cannot infer type arguments for call to generic "
			 "function %qs; specify them explicitly, e.g. f[int]"),
			Gogo::message_name(info->name()).c_str());
	  return NULL;
	}
      if (solved[i] == NULL
	  || !type_to_tokens(solved[i], type_args[i], location, &pkg_bindings))
	{
	  if (!quiet)
	    go_error_at(location,
			("cannot infer type arguments for call to generic "
			 "function %qs; specify them explicitly, e.g. f[int]"),
			Gogo::message_name(info->name()).c_str());
	  return NULL;
	}
    }

  Named_object* inst = this->instantiate_generic_function(info, type_args,
							  location,
							  &pkg_bindings);

  // Inference runs during the determine_types pass, after global names
  // have already been resolved once.  The freshly instantiated function
  // may contain new references to predeclared names (int, make, ...), so
  // resolve them and lower its builtin calls now (the global passes that
  // normally do this have already run).
  this->gogo_->resolve_global_names();
  if (inst != NULL)
    this->gogo_->lower_builtin_calls_for(inst);

  return inst;
}

// Generics: build (and cache) INFO's signature with the type parameters
// replaced by inference marker types, so it can be structurally unified
// against a concrete type.  Shared by call-argument inference and by
// context inference (a bare generic function used as a value).

Function_type*
Parse::generic_marker_signature(Generic_function_info* info)
{
  Function_type* gsig = info->marker_signature();
  if (gsig != NULL)
    return gsig;

  size_t nparams = info->type_param_names().size();
  std::vector<std::vector<Token> > marker_args(nparams);
  for (size_t k = 0; k < nparams; ++k)
    {
      this->gogo_->infer_marker_type(k);
      char buf[32];
      snprintf(buf, sizeof buf, "$infermarker%zu", k);
      marker_args[k].push_back(
	Token::make_identifier_token(std::string(buf), true,
				     Linemap::predeclared_location()));
    }
  std::vector<Token> subst;
  substitute_type_params(info->tokens(), info->type_param_names(),
			 marker_args, subst);

  this->gogo_->push_instantiation_context();
  bool imported_sig = info->defining_package() != NULL;
  if (imported_sig)
    this->gogo_->push_instantiation_package(info->defining_package());
  Parse sp(this->lex_, this->gogo_);
  sp.set_replay_tokens(&subst);
  sp.set_replay_pkg_aliases(&info->package_aliases());
  gsig = sp.signature(NULL, Linemap::predeclared_location());
  if (imported_sig)
    this->gogo_->pop_instantiation_package();
  this->gogo_->pop_instantiation_context();
  info->set_marker_signature(gsig);
  return gsig;
}

// Generics: instantiate a generic function used as a *value* (not called),
// inferring its type arguments from the CONTEXT function type it is being
// assigned to -- e.g. "var f func() T = GenFn" or "return GenFn" where
// GenFn is "func[N]() Foo[N]" and the context fixes N.  Unifies the generic
// function's parameter and result types against the context function type.
// Returns NULL (quietly) if inference does not fully solve the type
// parameters, so the caller can fall back to the ordinary error path.

Named_object*
Parse::instantiate_generic_from_context(Generic_function_info* info,
					Function_type* ctxt,
					Location location)
{
  if (ctxt == NULL)
    return NULL;
  size_t nparams = info->type_param_names().size();
  Function_type* gsig = this->generic_marker_signature(info);
  if (gsig == NULL)
    return NULL;

  // The generic function's arity must match the context function type, or
  // it cannot be the intended value.
  const Typed_identifier_list* gp = gsig->parameters();
  const Typed_identifier_list* cp = ctxt->parameters();
  const Typed_identifier_list* gr = gsig->results();
  const Typed_identifier_list* cr = ctxt->results();
  size_t gpn = (gp == NULL ? 0 : gp->size());
  size_t cpn = (cp == NULL ? 0 : cp->size());
  size_t grn = (gr == NULL ? 0 : gr->size());
  size_t crn = (cr == NULL ? 0 : cr->size());
  if (gpn != cpn || grn != crn)
    return NULL;

  std::vector<Type*> solved(nparams, (Type*) NULL);
  if (gp != NULL && cp != NULL)
    {
      Typed_identifier_list::const_iterator pi = gp->begin();
      Typed_identifier_list::const_iterator ci = cp->begin();
      for (; pi != gp->end() && ci != cp->end(); ++pi, ++ci)
	if (ci->type() != NULL && !ci->type()->is_error_type())
	  unify_marker(this->gogo_, pi->type(), ci->type(), solved, 0);
    }
  if (gr != NULL && cr != NULL)
    {
      Typed_identifier_list::const_iterator pi = gr->begin();
      Typed_identifier_list::const_iterator ci = cr->begin();
      for (; pi != gr->end() && ci != cr->end(); ++pi, ++ci)
	if (ci->type() != NULL && !ci->type()->is_error_type())
	  unify_marker(this->gogo_, pi->type(), ci->type(), solved, 0);
    }

  std::vector<std::vector<Token> > type_args(nparams);
  std::map<std::string, std::string> pkg_bindings;
  for (size_t i = 0; i < nparams; ++i)
    {
      if (solved[i] == NULL
	  || type_has_infer_marker(this->gogo_, solved[i], 0)
	  || !type_to_tokens(solved[i], type_args[i], location, &pkg_bindings))
	return NULL;
    }

  Named_object* inst = this->instantiate_generic_function(info, type_args,
							  location,
							  &pkg_bindings);
  this->gogo_->resolve_global_names();
  if (inst != NULL)
    this->gogo_->lower_builtin_calls_for(inst);
  return inst;
}

// Generics: record obligations that each type argument satisfy its type
// parameter's constraint.  Checked after types are determined.

static void
record_constraint_obligations(Gogo* gogo, Generic_function_info* info,
			      const std::vector<std::vector<Token> >& type_args,
			      const std::string& what, Location location,
			      const std::map<std::string, std::string>*
			        extra_pkg_aliases)
{
  std::vector<std::vector<Token> >& cons = info->constraints();
  // The alias map used when the obligation's tokens are re-parsed later: the
  // template's own imports, plus the caller's bindings for the type-argument
  // packages (template aliases win on a name collision).
  std::map<std::string, std::string> aliases = info->package_aliases();
  if (extra_pkg_aliases != NULL)
    aliases.insert(extra_pkg_aliases->begin(), extra_pkg_aliases->end());
  for (size_t i = 0; i < type_args.size() && i < cons.size(); ++i)
    {
      if (cons[i].empty())
	continue;
      Constraint_obligation* o = new Constraint_obligation;
      o->arg = type_args[i];
      o->constraint = cons[i];
      o->what = what;
      o->tparams = info->type_param_names();
      o->pkg_aliases = aliases;
      o->defining_package = info->defining_package();
      o->location = location;
      gogo->add_constraint_obligation(o);
    }
}

// Generics: a generic instance is re-parsed at package scope, so a type
// argument that names a function-local type would not resolve.  For each
// such argument, assign (once, cached) a package-scope alias name and
// rewrite the argument token to it; record (alias-name, local-type) pairs
// in ALIASES for define_localized_type_aliases to materialize.  Must be
// called while still in the calling function's scope.

// Generics: return a stable package-scope alias name for the
// function-local named type LT, creating the alias (a package-level type
// that is an alias of LT) on first use so that the name resolves when an
// instance is re-parsed at package scope.  LT must be a function-local
// named type.

std::string
Parse::package_alias_for_local_type(Type* lt, Location location)
{
  // Persistent across instantiations: a stable alias name per local type.
  static std::map<Named_object*, std::string> alias_names;

  Named_object* no = lt->named_type()->named_object();
  std::map<Named_object*, std::string>::iterator p = alias_names.find(no);
  if (p != alias_names.end())
    return p->second;

  char buf[40];
  snprintf(buf, sizeof buf, "$localtype%u", (unsigned) alias_names.size());
  std::string syn(buf);
  alias_names[no] = syn;

  // Create the alias at package scope.  We may be called in a function's
  // scope (explicit instantiation) or at package scope (inference); enter
  // the instantiation context if needed so the alias is package-level.
  bool pushed = false;
  if (!this->gogo_->in_global_scope())
    {
      this->gogo_->push_instantiation_context();
      pushed = true;
    }
  // A type reference packs an unexported name with the package path, so
  // declare under the packed name.
  std::string packed = this->gogo_->pack_hidden_name(syn, false);
  Named_object* ph = this->gogo_->declare_type(packed, location);
  Named_type* alias = Type::make_named_type(ph, lt, location);
  alias->set_is_alias();
  this->gogo_->define_type(ph, alias);
  if (pushed)
    this->gogo_->pop_instantiation_context();

  return syn;
}

// Generics: a solved type argument may be a composite (e.g. "[]le" or
// "map[k]le") that nests a function-local named type.  Such an embedded
// local type is invisible at the package scope where the instance is
// re-parsed, so walk the type and, for every function-local named type
// found, create a package-scope alias and record it in the instance-spelling
// map keyed by the local type's Named_object.  type_to_tokens consults that
// map and emits the alias wherever the local type appears (bare or nested).
// The walk descends only into composite structure, never into a named type's
// own definition, so self-referential types cannot cause it to loop; the
// depth cap is a further backstop.

void
Parse::register_local_type_aliases(Type* t, Location loc, int depth)
{
  if (t == NULL || depth > 64)
    return;
  t = t->forwarded();
  Named_type* nt = t->named_type();
  if (nt != NULL)
    {
      unsigned int idx;
      if (nt->in_function(&idx) != NULL)
	{
	  const Named_object* no = nt->named_object();
	  if (generic_instance_spelling.find(no)
	      == generic_instance_spelling.end())
	    {
	      std::string syn = this->package_alias_for_local_type(t, loc);
	      std::vector<Token> toks;
	      toks.push_back(Token::make_identifier_token(syn, false, loc));
	      generic_instance_spelling[no] = toks;
	    }
	}
      return;
    }
  if (t->points_to() != NULL)
    {
      this->register_local_type_aliases(t->points_to(), loc, depth + 1);
      return;
    }
  Array_type* at = t->array_type();
  if (at != NULL)
    {
      this->register_local_type_aliases(at->element_type(), loc, depth + 1);
      return;
    }
  Map_type* mt = t->map_type();
  if (mt != NULL)
    {
      this->register_local_type_aliases(mt->key_type(), loc, depth + 1);
      this->register_local_type_aliases(mt->val_type(), loc, depth + 1);
      return;
    }
  Channel_type* ct = t->channel_type();
  if (ct != NULL)
    {
      this->register_local_type_aliases(ct->element_type(), loc, depth + 1);
      return;
    }
  Struct_type* st = t->struct_type();
  if (st != NULL)
    {
      const Struct_field_list* fields = st->fields();
      if (fields != NULL)
	for (Struct_field_list::const_iterator pf = fields->begin();
	     pf != fields->end(); ++pf)
	  this->register_local_type_aliases(pf->type(), loc, depth + 1);
      return;
    }
  Function_type* ft = t->function_type();
  if (ft != NULL)
    {
      const Typed_identifier_list* params = ft->parameters();
      if (params != NULL)
	for (Typed_identifier_list::const_iterator pp = params->begin();
	     pp != params->end(); ++pp)
	  this->register_local_type_aliases(pp->type(), loc, depth + 1);
      const Typed_identifier_list* results = ft->results();
      if (results != NULL)
	for (Typed_identifier_list::const_iterator pr = results->begin();
	     pr != results->end(); ++pr)
	  this->register_local_type_aliases(pr->type(), loc, depth + 1);
      return;
    }
}

// Generics: while re-parsing an instantiated template (replay), a bare type
// name may belong to one of the templates whose instantiation is currently in
// progress rather than to the package being compiled.  Walk the whole
// instantiation-package stack (innermost first, skipping the current package)
// and resolve the name against each such package's bindings.  Returns the
// resolved type Named_object, or NULL if not found in any of them.  Used both
// for a bare type-argument name in type_name and for a bare unnamed-parameter
// type in parameter_list (e.g. resolver.Address as an argument to iter.Seq2
// inside a method of resolver.AddressMapV2[T], where the nested iter template
// is on top of the stack but Address belongs to the outer resolver package).

Named_object*
Parse::lookup_type_in_instantiation_packages(const std::string& name)
{
  Named_object* result = NULL;
  const std::vector<Package*>& ips = this->gogo_->instantiation_packages();
  for (std::vector<Package*>::const_reverse_iterator pi = ips.rbegin();
       pi != ips.rend() && result == NULL;
       ++pi)
    {
      Package* ip = *pi;
      if (ip == NULL || ip->pkgpath() == this->gogo_->pkgpath())
	continue;
      Named_object* ino = ip->bindings()->lookup(name);
      if (ino == NULL)
	{
	  std::string bare = Gogo::unpack_hidden_name(name);
	  if (!bare.empty())
	    {
	      ino = ip->bindings()->lookup(bare);
	      if (ino == NULL)
		ino = ip->bindings()->lookup('.' + ip->pkgpath() + '.' + bare);
	    }
	}
      if (ino != NULL && (ino->is_type() || ino->is_type_declaration()))
	result = ino;
    }
  return result;
}

// Generics: like lookup_type_in_instantiation_packages, but for a value
// reference (const/var/func) rather than a type.  While re-parsing an imported
// template, a bare unexported identifier belongs to the template's DEFINING
// package, not the package being compiled; but pack_hidden_name packs it with
// the compiling package's path, so an ordinary lookup finds a same-named
// object in the WRONG package (e.g. slices' and sort's identical unexported
// const "increasingHint" of their identical unexported type "sortedHint").
// Resolve the name against the instantiation-package stack (innermost first,
// skipping the compiling package) so it names the defining package's object.
// Returns NULL if not found there.

Named_object*
Parse::lookup_value_in_instantiation_packages(const std::string& name)
{
  Named_object* result = NULL;
  const std::vector<Package*>& ips = this->gogo_->instantiation_packages();
  for (std::vector<Package*>::const_reverse_iterator pi = ips.rbegin();
       pi != ips.rend() && result == NULL;
       ++pi)
    {
      Package* ip = *pi;
      if (ip == NULL || ip->pkgpath() == this->gogo_->pkgpath())
	continue;
      Named_object* ino = ip->bindings()->lookup(name);
      if (ino == NULL)
	{
	  std::string bare = Gogo::unpack_hidden_name(name);
	  if (!bare.empty())
	    {
	      ino = ip->bindings()->lookup(bare);
	      if (ino == NULL)
		ino = ip->bindings()->lookup('.' + ip->pkgpath() + '.' + bare);
	    }
	}
      // Accept any real object (const/var/func/type), but never a package.
      if (ino != NULL && !ino->is_package())
	result = ino;
    }
  return result;
}

// Generics: a generic instance is re-parsed at package scope, so a type
// argument that names a function-local type would not resolve.  Replace
// each such single-identifier argument with a package-scope alias name.
// Must be called while still in the calling function's scope.

void
Parse::localize_local_type_args(std::vector<std::vector<Token> >& type_args,
				std::map<std::string, std::string>* pkg_bindings)
{
  for (size_t i = 0; i < type_args.size(); ++i)
    {
      std::vector<Token>& a = type_args[i];
      if (a.empty())
	continue;
      Location aloc = a[0].location();

      // Fast path: a bare identifier naming a function-local type.  Replace it
      // with its stable "$localtypeN" synthetic name, which maps back to this
      // exact original type object during replay (see replay_local_type_obj /
      // type_name).  Must be done while the local type is still in scope (this
      // runs at the use site).  Emitted exported so the name is not
      // package-packed and resolves verbatim.
      if (a.size() == 1 && a[0].is_identifier())
	{
	  std::string id = a[0].identifier();
	  bool exp = a[0].is_identifier_exported();
	  std::string packed = this->gogo_->pack_hidden_name(id, exp);
	  Named_object* in_function = NULL;
	  Named_object* no = this->gogo_->lookup(packed, &in_function);
	  if (no != NULL && in_function != NULL && no->is_type())
	    {
	      Type* lt = no->type_value();
	      if (lt != NULL && !lt->is_error_type()
		  && lt->named_type() != NULL)
		{
		  std::string syn = this->package_alias_for_local_type(
		    lt, aloc);
		  a.clear();
		  a.push_back(Token::make_identifier_token(syn, false, aloc));
	      }
	    }
	  else if (no != NULL && no->is_type() && no->package() != NULL)
	    {
	      Type* lt = no->type_value();
	      if (lt != NULL && !lt->is_error_type())
		{
		  std::vector<Token> rewritten;
		  std::map<std::string, std::string> bindings;
		  if (type_to_tokens(lt, rewritten, aloc,
				     pkg_bindings != NULL ? &bindings : NULL)
		      && !rewritten.empty())
		    {
		      if (pkg_bindings != NULL && !bindings.empty())
			pkg_bindings->insert(bindings.begin(), bindings.end());
		      a.swap(rewritten);
		    }
		}
	    }
	  continue;
	}

      // General path: a composite/instance argument (e.g. "[]V", "map[K]V",
      // "X[int]" where X is a function-local generic type).  If it references a
      // function-local type anywhere, re-emit the whole argument via
      // type_to_tokens, which substitutes "$localtypeN" for each local part
      // (preserving identity) and leaves the rest verbatim.  Only done when a
      // local type is actually mentioned, to avoid eagerly resolving/rewriting
      // ordinary composite arguments.
      bool mentions_local = false;
      for (size_t j = 0; j < a.size() && !mentions_local; ++j)
	{
	  if (!a[j].is_identifier())
	    continue;
	  std::string packed =
	    this->gogo_->pack_hidden_name(a[j].identifier(),
					  a[j].is_identifier_exported());
	  Named_object* inf = NULL;
	  Named_object* n = this->gogo_->lookup(packed, &inf);
	  if (n != NULL && (n->is_type() || n->is_type_declaration())
	      && (inf != NULL || n->package() != NULL))
	    mentions_local = true;
	}
      if (!mentions_local)
	continue;
      Type* at = this->parse_type_from_tokens(a, NULL, false);
      if (at == NULL || at->is_error_type())
	continue;
      std::vector<Token> rewritten;
      std::map<std::string, std::string> bindings;
      if (type_to_tokens(at, rewritten, aloc,
			 pkg_bindings != NULL ? &bindings : NULL)
	  && !rewritten.empty())
	{
	  if (pkg_bindings != NULL && !bindings.empty())
	    pkg_bindings->insert(bindings.begin(), bindings.end());
	  a.swap(rewritten);
	}
    }
}

// Generics: instantiate a generic function template with the given type
// arguments (each a captured token sequence).  Returns the Named_object
// for the (possibly cached) instance, or NULL on error.

Named_object*
Parse::instantiate_generic_function(Generic_function_info* info,
				    const std::vector<std::vector<Token> >& type_args_in,
				    Location location,
				    const std::map<std::string, std::string>*
				      extra_pkg_aliases)
{
  // A type argument may name a function-local type, which is not visible at
  // the package scope where the instance is re-parsed.  Replace each such
  // argument with a package-scope alias (created after the instantiation
  // context is entered, below).  Done before the cache key is built so that
  // repeated uses share one instance.
  std::vector<std::vector<Token> > type_args = type_args_in;
  std::map<std::string, std::string> pkg_bindings;
  this->localize_local_type_args(type_args, &pkg_bindings);
  std::map<std::string, std::string> arg_aliases = pkg_bindings;
  if (extra_pkg_aliases != NULL && !extra_pkg_aliases->empty())
    arg_aliases.insert(extra_pkg_aliases->begin(), extra_pkg_aliases->end());

  // Build a mangled key from the type arguments and check the cache.
  std::string key = this->instance_key(type_args);
  Named_object* cached = info->find_instance(key);
  if (cached != NULL)
    return cached;

  if (type_args.size() != info->type_param_names().size())
    {
      go_error_at(location,
		  "wrong number of type arguments for generic function %qs",
		  Gogo::message_name(info->name()).c_str());
      return NULL;
    }

  record_constraint_obligations(this->gogo_, info, type_args,
				Gogo::message_name(info->name()), location,
				(!arg_aliases.empty() ? &arg_aliases
				 : NULL));

  // Substitute type arguments for type parameter names throughout the
  // captured token stream.
  std::vector<Token> substituted;
  substitute_type_params(info->tokens(), info->type_param_names(), type_args,
			 substituted);

  // Make a unique instance name.
  static unsigned int count;
  char buf[64];
  snprintf(buf, sizeof buf, "$inst%u", count);
  ++count;
  std::string iname = info->name() + std::string(buf);

  // Re-parse the substituted tokens as an ordinary, concrete function
  // at the top level.  We clear the function-parsing context first so
  // that the instance (and the lookups made while parsing it) behave as
  // a top-level declaration rather than a function nested in whatever
  // function is currently being parsed.
  this->gogo_->push_instantiation_context();
  bool imported = info->defining_package() != NULL;
  if (imported)
    this->gogo_->push_instantiation_package(info->defining_package());

  size_t defs_mark = this->gogo_->package_definitions_mark();

  Parse ip(this->lex_, this->gogo_);
  ip.set_replay_tokens(&substituted);
  // The replay resolves package qualifiers through the template's own
  // alias->pkgpath map, layered with the caller's type-argument bindings, so a
  // cross-package type argument whose package name the template does not import
  // (or is ambiguous by name) still resolves to the exact package it came from.
  std::map<std::string, std::string> merged_aliases;
  if (!arg_aliases.empty())
    {
      merged_aliases = info->package_aliases();
      merged_aliases.insert(arg_aliases.begin(), arg_aliases.end());
      ip.set_replay_pkg_aliases(&merged_aliases);
    }
  else
    ip.set_replay_pkg_aliases(&info->package_aliases());

  // A generic function is always declared at package scope (Go has no
  // function-local generic function declarations); push_instantiation_context
  // above cleared the caller's function stack, so this replay's
  // lookup_generic_type does not see any caller/enclosing function-local
  // generic templates (they live in the caller's Bindings) -- its generic-type
  // names resolve against package scope.  A function-local generic type
  // declared in THIS function's own body registers in this instance function's
  // Bindings and is discarded when the function's block is finished.
  Function_type* fntype = ip.signature(NULL, location);
  if (fntype == NULL)
    fntype = Type::make_function_type(NULL, NULL, NULL, location);

  Named_object* ino = this->gogo_->start_function(iname, fntype, false,
						  location);
  // Register before parsing the body so that recursive calls with the
  // same type arguments resolve to this instance.
  info->add_instance(key, ino, type_args);
  // Expose this instantiation's type arguments to the body replay, so a
  // function-local generic type declared in the body records them for its
  // reflection name.  Only meaningful when this function is itself generic.
  bool pushed_encl = !info->type_param_names().empty();
  if (pushed_encl)
    enclosing_generic_args_stack.push_back(type_args);
  ip.block();
  if (pushed_encl)
    enclosing_generic_args_stack.pop_back();
  this->gogo_->finish_function(location);
  if (imported)
    this->gogo_->pop_instantiation_package();
  this->gogo_->pop_instantiation_context();

  // When this instance is created during a late pass (e.g. type-argument
  // inference, which may nest further explicit instantiations such as a
  // generic function value Abs[T] passed as an argument), the global
  // name-resolution and builtin-lowering passes have already run.  The
  // fresh body may reference predeclared names and builtin calls (make,
  // complex, ...), so resolve and lower them now.
  if (this->gogo_->parsing_complete())
    {
      this->gogo_->resolve_global_names();
      this->gogo_->lower_builtin_calls_since(defs_mark);
    }

  return ino;
}

// Receiver = Parameters .

Typed_identifier*
Parse::receiver()
{
  Location location = this->location();
  Typed_identifier_list* til;
  if (!this->parameters(&til, NULL))
    return NULL;
  else if (til == NULL || til->empty())
    {
      go_error_at(location, "method has no receiver");
      return NULL;
    }
  else if (til->size() > 1)
    {
      go_error_at(location, "method has multiple receivers");
      return NULL;
    }
  else
    return &til->front();
}

// Operand    = Literal | QualifiedIdent | MethodExpr | "(" Expression ")" .
// Literal    = BasicLit | CompositeLit | FunctionLit .
// BasicLit   = int_lit | float_lit | imaginary_lit | char_lit | string_lit .

// If MAY_BE_SINK is true, this operand may be "_".

// If IS_PARENTHESIZED is not NULL, *IS_PARENTHESIZED is set to true
// if the entire expression is in parentheses.

Expression*
Parse::operand(bool may_be_sink, bool* is_parenthesized)
{
  const Token* token = this->peek_token();
  Expression* ret;
  switch (token->classification())
    {
    case Token::TOKEN_IDENTIFIER:
      {
	Location location = token->location();
	std::string id = token->identifier();
	bool is_exported = token->is_identifier_exported();
	std::string packed = this->gogo_->pack_hidden_name(id, is_exported);

	Named_object* in_function;
	Named_object* named_object = this->gogo_->lookup(packed, &in_function);

	// Generics: while re-parsing an imported template, a bare UNEXPORTED
	// identifier belongs to the template's DEFINING package, not the package
	// being compiled.  pack_hidden_name packed it with the compiling
	// package's path, so the lookup above may have found a same-named object
	// in the WRONG package (e.g. slices' and sort's identical unexported
	// const "increasingHint" of their identical unexported type
	// "sortedHint"), giving a type mismatch inside the instantiated body.
	// Prefer the instantiation package's binding, mirroring type_name's stack
	// walk.  Skip synthetic names ($infermarker/$localtype), function-local
	// names, and package qualifiers, which are current-context.
	if (this->replay_tokens_ != NULL
	    && !is_exported
	    && in_function == NULL
	    && (named_object == NULL || !named_object->is_package())
	    && (id.empty() || id[0] != '$'))
	  {
	    Named_object* ino = this->lookup_value_in_instantiation_packages(id);
	    if (ino != NULL)
	      named_object = ino;
	  }

	// While re-parsing a generic instance, resolve a package qualifier
	// through the template's alias->pkgpath map, so it names the exact
	// package the template was defined against rather than an ambiguous
	// same-named package imported elsewhere (mirrors qualified_ident).
	// The map is authoritative for a package qualifier: an ordinary by-name
	// lookup may have found a different same-named package, or a package-LEVEL
	// object in the instantiating package that shadows the package name (e.g. a
	// local "func cmp" shadowing the "cmp" package while the template body calls
	// cmp.Less), so override it.  The identifier is present in the alias map only
	// if the template used it as a package qualifier, so resolving it to that
	// package here is correct.  But do not override a name that resolves to a
	// genuine function-scoped local (in_function != NULL) -- e.g. a parameter
	// "errors error" shadowing the "errors" package -- which is a value and must
	// be kept.
	if (this->replay_pkg_aliases_ != NULL
	    && in_function == NULL)
	  {
	    std::map<std::string, std::string>::const_iterator a =
	      this->replay_pkg_aliases_->find(id);
	    if (a != this->replay_pkg_aliases_->end())
	      {
		Named_object* pno =
		  this->gogo_->package_no_for_pkgpath(a->second);
		if (pno != NULL)
		  {
		    pno->package_value()->add_alias(
		      id, Linemap::unknown_location());
		    named_object = pno;
		    in_function = NULL;
		  }
	      }
	  }

	Package* package = NULL;
	if (named_object != NULL && named_object->is_package())
	  {
	    if (this->advance_token()->is_op(OPERATOR_DOT))
	      {
		if (!this->advance_token()->is_identifier())
		  {
		    go_error_at(location, "unexpected reference to package");
		    return Expression::make_error(location);
		  }
		package = named_object->package_value();
		package->note_usage(id);
		id = this->peek_token()->identifier();
		is_exported = this->peek_token()->is_identifier_exported();
		packed = this->gogo_->pack_hidden_name(id, is_exported);
		named_object = package->lookup(packed);
		location = this->location();
		go_assert(in_function == NULL);
	      }
	    else
	      {
		// The name resolved to a package but is not followed by
		// ".Name", so it is being used as an ordinary value.  This
		// happens when a package-scope identifier (e.g. a
		// "var testlog = ..." in the testing package) shares its
		// basename with an imported package (internal/testlog) and was
		// resolved to a synthesized package by the generics last-resort
		// lookup in Gogo::lookup.  Put the identifier back and treat it
		// as an ordinary, possibly not-yet-declared, name.
		this->unget_token(Token::make_identifier_token(id, is_exported,
							       location));
		named_object = NULL;
	      }
	  }

	this->advance_token();

	// Generics: a generic type used in expression context, e.g. a
	// composite literal Pair[int,string]{...} or a conversion
	// T[int](x).  Instantiate the type and return it as a type
	// expression; primary_expr will handle the following "{" or "(".
	if (package == NULL
	    && this->peek_token()->is_op(OPERATOR_LSQUARE))
	  {
	    Generic_function_info* ginfo =
	      this->gogo_->lookup_generic_type(packed);
	    // While re-parsing an imported template, a bare reference to one of
	    // the defining package's own (possibly unexported) generic types
	    // (e.g. "&filter[N]{...}" inside its NewFilter constructor) was
	    // packed with the importing package's pkgpath; also try the current
	    // instantiation package's pkgpath (mirrors type_name).
	    if (ginfo == NULL)
	      {
		Package* ip = this->gogo_->current_instantiation_package();
		if (ip != NULL)
		  ginfo = this->gogo_->lookup_generic_type(
		    ip->pkgpath() + '.' + Gogo::unpack_hidden_name(packed));
	      }
	    if (ginfo != NULL)
	      {
		Type* t = this->generic_type_instantiation(ginfo, location);
		return Expression::make_type(t, location);
	      }
	  }

	// Generics: a generic type imported from another package used in
	// expression context, e.g. mylib.Pair[int]{...}.
	if (package != NULL
	    && this->peek_token()->is_op(OPERATOR_LSQUARE))
	  {
	    Generic_function_info* ginfo =
	      this->gogo_->lookup_generic_type(package->pkgpath() + '.' + id);
	    if (ginfo != NULL)
	      {
		Type* t = this->generic_type_instantiation(ginfo, location);
		return Expression::make_type(t, location);
	      }
	  }

	if (named_object != NULL
	    && named_object->is_type()
	    && !named_object->type_value()->is_visible())
	  {
	    // While re-parsing an instantiated imported generic body, an
	    // unqualified reference to one of the defining package's own
	    // unexported helper types is legal and may resolve here with no
	    // package qualifier.  Mirror type_name and accept replay-time uses.
	    if (this->replay_tokens_ == NULL)
	      {
		const Package* p = package;
		if (p == NULL)
		  p = named_object->package();
		if (p != NULL)
		  go_error_at(location,
			      "invalid reference to hidden type %<%s.%s%>",
			      Gogo::message_name(p->package_name()).c_str(),
			      Gogo::message_name(id).c_str());
		else
		  go_error_at(location,
			      "invalid reference to hidden type %<%s%>",
			      Gogo::message_name(id).c_str());
		return Expression::make_error(location);
	      }
	  }


	if (named_object == NULL)
	  {
	    if (package != NULL)
	      {
		std::string n1 = Gogo::message_name(package->package_name());
		std::string n2 = Gogo::message_name(id);
		if (!is_exported)
		  go_error_at(location,
			      ("invalid reference to unexported identifier "
			       "%<%s.%s%>"),
			      n1.c_str(), n2.c_str());
		else
		  go_error_at(location,
			      "reference to undefined identifier %<%s.%s%>",
			      n1.c_str(), n2.c_str());
		return Expression::make_error(location);
	      }

	    named_object = this->gogo_->add_unknown_name(packed, location);
	  }

	if (in_function != NULL
	    && in_function != this->gogo_->current_function()
	    && (named_object->is_variable()
		|| named_object->is_result_variable()))
	  return this->enclosing_var_reference(in_function, named_object,
					       may_be_sink, location);

	switch (named_object->classification())
	  {
	  case Named_object::NAMED_OBJECT_CONST:
	    return Expression::make_const_reference(named_object, location);
	  case Named_object::NAMED_OBJECT_TYPE:
	    return Expression::make_type(named_object->type_value(), location);
	  case Named_object::NAMED_OBJECT_TYPE_DECLARATION:
	    {
	      Type* t = Type::make_forward_declaration(named_object);
	      return Expression::make_type(t, location);
	    }
	  case Named_object::NAMED_OBJECT_VAR:
	  case Named_object::NAMED_OBJECT_RESULT_VAR:
	    // Any left-hand-side can be a sink, so if this can not be
	    // a sink, then it must be a use of the variable.
	    if (!may_be_sink)
	      this->mark_var_used(named_object);
	    return Expression::make_var_reference(named_object, location);
	  case Named_object::NAMED_OBJECT_SINK:
	    if (may_be_sink)
	      return Expression::make_sink(location);
	    else
	      {
		go_error_at(location, "cannot use %<_%> as value");
		return Expression::make_error(location);
	      }
	  case Named_object::NAMED_OBJECT_FUNC:
	  case Named_object::NAMED_OBJECT_FUNC_DECLARATION:
	    return Expression::make_func_reference(named_object, NULL,
						   location);
	  case Named_object::NAMED_OBJECT_UNKNOWN:
	    {
	      Unknown_expression* ue =
		Expression::make_unknown_reference(named_object, location);
	      if (this->is_erroneous_function_)
		ue->set_no_error_message();
	      return ue;
	    }
	  case Named_object::NAMED_OBJECT_ERRONEOUS:
	    return Expression::make_error(location);
	  default:
	    go_unreachable();
	  }
      }
      go_unreachable();

    case Token::TOKEN_STRING:
      ret = Expression::make_string(token->string_value(), token->location());
      this->advance_token();
      return ret;

    case Token::TOKEN_CHARACTER:
      ret = Expression::make_character(token->character_value(), NULL,
				       token->location());
      this->advance_token();
      return ret;

    case Token::TOKEN_INTEGER:
      ret = Expression::make_integer_z(token->integer_value(), NULL,
				       token->location());
      this->advance_token();
      return ret;

    case Token::TOKEN_FLOAT:
      ret = Expression::make_float(token->float_value(), NULL,
				   token->location());
      this->advance_token();
      return ret;

    case Token::TOKEN_IMAGINARY:
      {
	mpfr_t zero;
	mpfr_init_set_ui(zero, 0, MPFR_RNDN);
	mpc_t val;
	mpc_init2(val, mpc_precision);
	mpc_set_fr_fr(val, zero, *token->imaginary_value(), MPC_RNDNN);
	mpfr_clear(zero);
	ret = Expression::make_complex(&val, NULL, token->location());
	mpc_clear(val);
	this->advance_token();
	return ret;
      }

    case Token::TOKEN_KEYWORD:
      switch (token->keyword())
	{
	case KEYWORD_FUNC:
	  return this->function_lit();
	case KEYWORD_CHAN:
	case KEYWORD_INTERFACE:
	case KEYWORD_MAP:
	case KEYWORD_STRUCT:
	  {
	    Location location = token->location();
	    return Expression::make_type(this->type(), location);
	  }
	default:
	  break;
	}
      break;

    case Token::TOKEN_OPERATOR:
      if (token->is_op(OPERATOR_LPAREN))
	{
	  this->advance_token();
	  ret = this->expression(PRECEDENCE_NORMAL, may_be_sink, true, NULL,
				 NULL);
	  if (!this->peek_token()->is_op(OPERATOR_RPAREN))
	    go_error_at(this->location(), "missing %<)%>");
	  else
	    this->advance_token();
	  if (is_parenthesized != NULL)
	    *is_parenthesized = true;
	  return ret;
	}
      else if (token->is_op(OPERATOR_LSQUARE))
	{
	  // Here we call array_type directly, as this is the only
	  // case where an ellipsis is permitted for an array type.
	  Location location = token->location();
	  return Expression::make_type(this->array_type(true), location);
	}
      break;

    default:
      break;
    }

  go_error_at(this->location(), "expected operand");
  return Expression::make_error(this->location());
}

// Handle a reference to a variable in an enclosing function.  We add
// it to a list of such variables.  We return a reference to a field
// in a struct which will be passed on the static chain when calling
// the current function.

Expression*
Parse::enclosing_var_reference(Named_object* in_function, Named_object* var,
			       bool may_be_sink, Location location)
{
  go_assert(var->is_variable() || var->is_result_variable());

  // Any left-hand-side can be a sink, so if this can not be
  // a sink, then it must be a use of the variable.
  if (!may_be_sink)
    this->mark_var_used(var);

  Named_object* this_function = this->gogo_->current_function();
  Named_object* closure = this_function->func_value()->closure_var();

  // The last argument to the Enclosing_var constructor is the index
  // of this variable in the closure.  We add 1 to the current number
  // of enclosed variables, because the first field in the closure
  // points to the function code.  Use the active set, which for a nested
  // index re-parse is the outer parse's set, so that references shared
  // across the re-parse are de-duplicated and indexed consistently with
  // the closure construction.
  Enclosing_vars& evs = this->active_enclosing_vars();
  Enclosing_var ev(var, in_function, evs.size() + 1);
  std::pair<Enclosing_vars::iterator, bool> ins = evs.insert(ev);
  if (ins.second)
    {
      // This is a variable we have not seen before.  Add a new field
      // to the closure type.
      this_function->func_value()->add_closure_field(var, location);
    }

  Expression* closure_ref = Expression::make_var_reference(closure,
							   location);
  closure_ref =
      Expression::make_dereference(closure_ref,
                                   Expression::NIL_CHECK_NOT_NEEDED,
                                   location);

  // The closure structure holds pointers to the variables, so we need
  // to introduce an indirection.
  Expression* e = Expression::make_field_reference(closure_ref,
						   ins.first->index(),
						   location);
  e = Expression::make_dereference(e, Expression::NIL_CHECK_NOT_NEEDED,
                                   location);
  return Expression::make_enclosing_var_reference(e, var, location);
}

// CompositeLit  = LiteralType LiteralValue .
// LiteralType   = StructType | ArrayType | "[" "..." "]" ElementType |
//                 SliceType | MapType | TypeName .
// LiteralValue  = "{" [ ElementList [ "," ] ] "}" .
// ElementList   = Element { "," Element } .
// Element       = [ Key ":" ] Value .
// Key           = FieldName | ElementIndex .
// FieldName     = identifier .
// ElementIndex  = Expression .
// Value         = Expression | LiteralValue .

// We have already seen the type if there is one, and we are now
// looking at the LiteralValue.  The case "[" "..."  "]" ElementType
// will be seen here as an array type whose length is "nil".  The
// DEPTH parameter is non-zero if this is an embedded composite
// literal and the type was omitted.  It gives the number of steps up
// to the type which was provided.  E.g., in [][]int{{1}} it will be
// 1.  In [][][]int{{{1}}} it will be 2.

Expression*
Parse::composite_lit(Type* type, int depth, Location location)
{
  go_assert(this->peek_token()->is_op(OPERATOR_LCURLY));
  this->advance_token();

  if (this->peek_token()->is_op(OPERATOR_RCURLY))
    {
      this->advance_token();
      return Expression::make_composite_literal(type, depth, false, NULL,
						false, location);
    }

  bool has_keys = false;
  bool all_are_names = true;
  Expression_list* vals = new Expression_list;
  while (true)
    {
      Expression* val;
      bool is_type_omitted = false;
      bool is_name = false;

      const Token* token = this->peek_token();

      if (token->is_identifier())
	{
	  std::string identifier = token->identifier();
	  bool is_exported = token->is_identifier_exported();
	  Location id_location = token->location();

	  if (this->advance_token()->is_op(OPERATOR_COLON))
	    {
	      // This may be a field name.  We don't know for sure--it
	      // could also be an expression for an array index.  We
	      // don't want to parse it as an expression because may
	      // trigger various errors, e.g., if this identifier
	      // happens to be the name of a package.
	      Gogo* gogo = this->gogo_;
	      val = this->id_to_expression(gogo->pack_hidden_name(identifier,
								  is_exported),
					   id_location, false, true);
	      is_name = true;
	    }
	  else
	    {
	      this->unget_token(Token::make_identifier_token(identifier,
							     is_exported,
							     id_location));
	      val = this->expression(PRECEDENCE_NORMAL, false, true, NULL,
				     NULL);
	    }
	}
      else if (!token->is_op(OPERATOR_LCURLY))
	val = this->expression(PRECEDENCE_NORMAL, false, true, NULL, NULL);
      else
	{
	  // This must be a composite literal inside another composite
	  // literal, with the type omitted for the inner one.
	  val = this->composite_lit(type, depth + 1, token->location());
          is_type_omitted = true;
	}

      token = this->peek_token();
      if (!token->is_op(OPERATOR_COLON))
	{
	  if (has_keys)
	    vals->push_back(NULL);
	  is_name = false;
	}
      else
	{
          if (is_type_omitted)
            {
              // VAL is a nested composite literal with an omitted type being
              // used a key.  Record this information in VAL so that the correct
              // type is associated with the literal value if VAL is a
              // map literal.
              val->complit()->update_key_path(depth);
            }

	  this->advance_token();

	  if (!has_keys && !vals->empty())
	    {
	      Expression_list* newvals = new Expression_list;
	      for (Expression_list::const_iterator p = vals->begin();
		   p != vals->end();
		   ++p)
		{
		  newvals->push_back(NULL);
		  newvals->push_back(*p);
		}
	      delete vals;
	      vals = newvals;
	    }
	  has_keys = true;

	  vals->push_back(val);

	  if (!token->is_op(OPERATOR_LCURLY))
	    val = this->expression(PRECEDENCE_NORMAL, false, true, NULL, NULL);
	  else
	    {
	      // This must be a composite literal inside another
	      // composite literal, with the type omitted for the
	      // inner one.
	      val = this->composite_lit(type, depth + 1, token->location());
	    }

	  token = this->peek_token();
	}

      vals->push_back(val);

      if (!is_name)
	all_are_names = false;

      if (token->is_op(OPERATOR_COMMA))
	{
	  if (this->advance_token()->is_op(OPERATOR_RCURLY))
	    {
	      this->advance_token();
	      break;
	    }
	}
      else if (token->is_op(OPERATOR_RCURLY))
	{
	  this->advance_token();
	  break;
	}
      else
	{
	  if (token->is_op(OPERATOR_SEMICOLON))
	    go_error_at(this->location(),
			("need trailing comma before newline "
			 "in composite literal"));
	  else
	    go_error_at(this->location(), "expected %<,%> or %<}%>");

	  this->gogo_->mark_locals_used();
	  int edepth = 0;
	  while (!token->is_eof()
		 && (edepth > 0 || !token->is_op(OPERATOR_RCURLY)))
	    {
	      if (token->is_op(OPERATOR_LCURLY))
		++edepth;
	      else if (token->is_op(OPERATOR_RCURLY))
		--edepth;
	      token = this->advance_token();
	    }
	  if (token->is_op(OPERATOR_RCURLY))
	    this->advance_token();

	  return Expression::make_error(location);
	}
    }

  Expression* cl =
    Expression::make_composite_literal(type, depth, has_keys, vals,
				       all_are_names, location);
  // In a generic instantiation, map keys that coincide after type-argument
  // substitution must not be reported as duplicates.  Record the defining
  // package's pkgpath (when instantiating an imported template) so that the
  // literal, which is that package's own code, may assign to its unexported
  // struct fields.
  if (this->replay_tokens_ != NULL && cl->complit() != NULL)
    {
      Package* ip = this->gogo_->current_instantiation_package();
      cl->complit()->set_is_instantiated(ip != NULL ? ip->pkgpath()
						    : std::string());
    }
  return cl;
}

// FunctionLit = "func" Signature Block .

Expression*
Parse::function_lit()
{
  Location location = this->location();
  go_assert(this->peek_token()->is_keyword(KEYWORD_FUNC));
  this->advance_token();

  Enclosing_vars hold_enclosing_vars;
  hold_enclosing_vars.swap(this->enclosing_vars_);
  // This function literal has its own closure, so its body must track its
  // own enclosing-variable references even if we are inside a nested index
  // re-parse that is sharing an outer set.
  Enclosing_vars* hold_shared_enclosing_vars = this->shared_enclosing_vars_;
  this->shared_enclosing_vars_ = NULL;

  Function_type* type = this->signature(NULL, location);
  bool fntype_is_error = false;
  if (type == NULL)
    {
      type = Type::make_function_type(NULL, NULL, NULL, location);
      fntype_is_error = true;
    }

  // For a function literal, the next token must be a '{'.  If we
  // don't see that, then we may have a type expression.
  if (!this->peek_token()->is_op(OPERATOR_LCURLY))
    {
      this->shared_enclosing_vars_ = hold_shared_enclosing_vars;
      hold_enclosing_vars.swap(this->enclosing_vars_);
      return Expression::make_type(type, location);
    }

  bool hold_is_erroneous_function = this->is_erroneous_function_;
  if (fntype_is_error)
    this->is_erroneous_function_ = true;

  Bc_stack* hold_break_stack = this->break_stack_;
  Bc_stack* hold_continue_stack = this->continue_stack_;
  this->break_stack_ = NULL;
  this->continue_stack_ = NULL;

  Named_object* no = this->gogo_->start_function("", type, true, location);

  Location end_loc = this->block();

  this->gogo_->finish_function(end_loc);

  if (this->break_stack_ != NULL)
    delete this->break_stack_;
  if (this->continue_stack_ != NULL)
    delete this->continue_stack_;
  this->break_stack_ = hold_break_stack;
  this->continue_stack_ = hold_continue_stack;

  this->is_erroneous_function_ = hold_is_erroneous_function;

  this->shared_enclosing_vars_ = hold_shared_enclosing_vars;
  hold_enclosing_vars.swap(this->enclosing_vars_);

  Expression* closure = this->create_closure(no, &hold_enclosing_vars,
					     location);

  return Expression::make_func_reference(no, closure, location);
}

// Create a closure for the nested function FUNCTION.  This is based
// on ENCLOSING_VARS, which is a list of all variables defined in
// enclosing functions and referenced from FUNCTION.  A closure is the
// address of a struct which point to the real function code and
// contains the addresses of all the referenced variables.  This
// returns NULL if no closure is required.

Expression*
Parse::create_closure(Named_object* function, Enclosing_vars* enclosing_vars,
		      Location location)
{
  if (enclosing_vars->empty())
    return NULL;

  // Get the variables in order by their field index.

  size_t enclosing_var_count = enclosing_vars->size();
  std::vector<Enclosing_var> ev(enclosing_var_count);
  for (Enclosing_vars::const_iterator p = enclosing_vars->begin();
       p != enclosing_vars->end();
       ++p)
    {
      // Subtract 1 because index 0 is the function code.
      ev[p->index() - 1] = *p;
    }

  // Build an initializer for a composite literal of the closure's
  // type.

  Named_object* enclosing_function = this->gogo_->current_function();
  Expression_list* initializer = new Expression_list;

  initializer->push_back(Expression::make_func_code_reference(function,
							      location));

  for (size_t i = 0; i < enclosing_var_count; ++i)
    {
      // Add 1 to i because the first field in the closure is a
      // pointer to the function code.
      go_assert(ev[i].index() == i + 1);
      Named_object* var = ev[i].var();
      Expression* ref;
      if (ev[i].in_function() == enclosing_function)
	ref = Expression::make_var_reference(var, location);
      else
	ref = this->enclosing_var_reference(ev[i].in_function(), var,
					    true, location);
      Expression* refaddr = Expression::make_unary(OPERATOR_AND, ref,
						   location);
      initializer->push_back(refaddr);
    }

  Named_object* closure_var = function->func_value()->closure_var();
  Struct_type* st = closure_var->var_value()->type()->deref()->struct_type();
  Expression* cv = Expression::make_struct_composite_literal(st, initializer,
							     location);
  return Expression::make_heap_expression(cv, location);
}

// PrimaryExpr = Operand { Selector | Index | Slice | TypeGuard | Call } .

// If MAY_BE_SINK is true, this expression may be "_".

// If MAY_BE_COMPOSITE_LIT is true, this expression may be a composite
// literal.

// If IS_TYPE_SWITCH is not NULL, this will recognize a type switch
// guard (var := expr.("type") using the literal keyword "type").

// If IS_PARENTHESIZED is not NULL, *IS_PARENTHESIZED is set to true
// if the entire expression is in parentheses.

Expression*
Parse::primary_expr(bool may_be_sink, bool may_be_composite_lit,
		    bool* is_type_switch, bool* is_parenthesized)
{
  Location start_loc = this->location();
  bool operand_is_parenthesized = false;
  bool whole_is_parenthesized = false;

  Expression* ret = this->operand(may_be_sink, &operand_is_parenthesized);

  whole_is_parenthesized = operand_is_parenthesized;

  // An unknown name followed by a curly brace must be a composite
  // literal, and the unknown name must be a type.
  if (may_be_composite_lit
      && !operand_is_parenthesized
      && ret->unknown_expression() != NULL
      && this->peek_token()->is_op(OPERATOR_LCURLY))
    {
      Named_object* no = ret->unknown_expression()->named_object();
      Type* type = Type::make_forward_declaration(no);
      ret = Expression::make_type(type, ret->location());
    }

  // We handle composite literals and type casts here, as it is the
  // easiest way to handle types which are in parentheses, as in
  // "((uint))(1)".
  if (ret->is_type_expression())
    {
      if (this->peek_token()->is_op(OPERATOR_LCURLY))
	{
	  whole_is_parenthesized = false;
	  if (!may_be_composite_lit)
	    {
	      Type* t = ret->type();
	      if (t->named_type() != NULL
		  || t->forward_declaration_type() != NULL)
		go_error_at(start_loc,
			    _("parentheses required around this composite "
			      "literal to avoid parsing ambiguity"));
	    }
	  else if (operand_is_parenthesized)
	    go_error_at(start_loc,
			"cannot parenthesize type in composite literal");
	  ret = this->composite_lit(ret->type(), 0, ret->location());
	}
      else if (this->peek_token()->is_op(OPERATOR_LPAREN))
	{
	  whole_is_parenthesized = false;
	  Location loc = this->location();
	  this->advance_token();
	  Expression* expr = this->expression(PRECEDENCE_NORMAL, false, true,
					      NULL, NULL);
	  if (this->peek_token()->is_op(OPERATOR_COMMA))
	    this->advance_token();
	  if (this->peek_token()->is_op(OPERATOR_ELLIPSIS))
	    {
	      go_error_at(this->location(),
			  "invalid use of %<...%> in type conversion");
	      this->advance_token();
	    }
	  if (!this->peek_token()->is_op(OPERATOR_RPAREN))
	    go_error_at(this->location(), "expected %<)%>");
	  else
	    this->advance_token();
	  if (expr->is_error_expression())
	    ret = expr;
	  else
	    {
	      Type* t = ret->type();
	      if (t->classification() == Type::TYPE_ARRAY
		  && t->array_type()->length() != NULL
		  && t->array_type()->length()->is_nil_expression())
		{
		  go_error_at(ret->location(),
			      "use of %<[...]%> outside of array literal");
		  ret = Expression::make_error(loc);
		}
	      else
		ret = Expression::make_cast(t, expr, loc);
	    }
	}
    }

  while (true)
    {
      const Token* token = this->peek_token();
      if (token->is_op(OPERATOR_LPAREN))
	{
	  whole_is_parenthesized = false;
	  ret = this->call(this->verify_not_sink(ret));
	}
      else if (token->is_op(OPERATOR_DOT))
	{
	  whole_is_parenthesized = false;
	  ret = this->selector(this->verify_not_sink(ret), is_type_switch);
	  if (is_type_switch != NULL && *is_type_switch)
	    break;
	}
      else if (token->is_op(OPERATOR_LSQUARE))
	{
	  whole_is_parenthesized = false;
	  // Generics: if RET refers to a generic function, "[...]" is a
	  // type argument list, so instantiate; otherwise it is an
	  // index/slice expression.
	  Generic_function_info* ginfo = NULL;
	  Func_expression* fe = ret->func_expression();
	  if (fe != NULL)
	    ginfo =
	      this->gogo_->lookup_generic_function_no(fe->named_object());
	  // An unknown reference may name a generic function that is already
	  // registered -- in particular while re-parsing an instance body that
	  // calls another generic function ("_Ranger[K]()"), where the name
	  // resolves to an unknown rather than a function reference.
	  if (ginfo == NULL && ret->unknown_expression() != NULL)
	    ginfo = this->gogo_->lookup_generic_function_no(
	      ret->unknown_expression()->named_object());
	  if (ginfo != NULL)
	    ret = this->generic_instantiation(ginfo, ret, ret->location());
	  else if (ret->unknown_expression() != NULL)
	    {
	      // A forward reference to a generic function used with explicit
	      // type arguments ("F[int](x)" where F is declared later) is not
	      // yet known to be generic.  Capture the bracket; if it is a
	      // type-argument list (it has a comma, or a call follows) defer
	      // it and let the call resolve it, otherwise treat it as an
	      // ordinary index/slice.
	      Location bl = ret->location();
	      std::string uname = ret->unknown_expression()->name();
	      std::vector<std::vector<Token> > groups;
	      std::vector<Token> rawb;
	      this->capture_bracketed_type_args(&groups, &rawb);
	      bool call_follows = this->peek_token()->is_op(OPERATOR_LPAREN);
	      bool brace_follows = this->peek_token()->is_op(OPERATOR_LCURLY);
	      if (brace_follows && may_be_composite_lit)
		{
		  // "F[args]{...}" where F is a generic type declared later:
		  // a composite literal of the (forward-referenced) generic
		  // type instance F[args].
		  Type* t = this->make_pending_generic_type(
		    Gogo::unpack_hidden_name(uname), groups, bl);
		  ret = this->composite_lit(t, 0, bl);
		}
	      else if (groups.size() > 1 || call_follows)
		{
		  partial_generic_type_args[ret] = groups;
		  // For a single-bracket call "name[expr](args)" whose bracket
		  // content is not unambiguously a type, "[expr]" is most
		  // likely an ordinary index of a value followed by a call
		  // (e.g. an array of functions, "transitionFunc[c.state](...)")
		  // rather than a generic instantiation "F[T](args)".  Record
		  // the index interpretation as a fallback so that if the name
		  // resolves to a non-generic value rather than a generic
		  // function, Call_expression::do_determine_type recovers it.
		  if (call_follows && groups.size() == 1
		      && !this->group_is_clearly_type(groups[0]))
		    {
		      std::vector<Token> rb = rawb;
		      rb.push_back(Token::make_eof_token(bl));
		      Parse ip(this->lex_, this->gogo_);
		      ip.set_replay_tokens(&rb);
		      ip.set_shared_enclosing_vars(&this->active_enclosing_vars());
		      partial_generic_call_fallback[ret] = ip.index(ret->copy());
		    }
		}
	      else if (this->group_is_clearly_type(groups[0]))
		{
		  // A single bracket whose content is unambiguously a type on a
		  // forward (unknown) reference, with no call or composite
		  // literal: this is an instantiation of a generic function used
		  // as a value ("F[int]") if the name turns out to be a generic
		  // function, or otherwise an index.  Build the index as a
		  // fallback and record both interpretations on the (kept)
		  // unknown reference; Unknown_expression::do_determine_type
		  // chooses between them.  Only done when the content is clearly
		  // a type, so an ordinary index ("v[i]", including the
		  // comma-ok map form) keeps its index expression at parse time.
		  std::vector<Token> rb = rawb;
		  rb.push_back(Token::make_eof_token(bl));
		  Parse ip(this->lex_, this->gogo_);
		  ip.set_replay_tokens(&rb);
		  ip.set_shared_enclosing_vars(&this->active_enclosing_vars());
		  Expression* fallback = ip.index(ret->copy());
		  generic_value_type_args[ret] = groups;
		  generic_value_fallback[ret] = fallback;
		}
	      else
		{
		  std::vector<Token> rb = rawb;
		  rb.push_back(Token::make_eof_token(bl));
		  Parse ip(this->lex_, this->gogo_);
		  ip.set_replay_tokens(&rb);
		  ip.set_shared_enclosing_vars(&this->active_enclosing_vars());
		  ret = ip.index(this->verify_not_sink(ret));
		}
	    }
	  else
	    ret = this->index(this->verify_not_sink(ret));
	}
      else
	break;
    }

  if (whole_is_parenthesized && is_parenthesized != NULL)
    *is_parenthesized = true;

  return ret;
}

// Generics: capture a bracketed "[a, b, ...]" list (current token is
// "[") into comma-separated token groups and the raw bracket tokens.

void
Parse::capture_bracketed_type_args(std::vector<std::vector<Token> >* groups,
				   std::vector<Token>* raw)
{
  go_assert(this->peek_token()->is_op(OPERATOR_LSQUARE));
  raw->push_back(*this->peek_token());
  this->advance_token();

  while (!this->peek_token()->is_op(OPERATOR_RSQUARE)
	 && !this->peek_token()->is_eof())
    {
      std::vector<Token> arg;
      int depth = 0;
      while (true)
	{
	  const Token* t = this->peek_token();
	  if (t->is_eof())
	    break;
	  if (depth == 0
	      && (t->is_op(OPERATOR_COMMA) || t->is_op(OPERATOR_RSQUARE)))
	    break;
	  if (t->is_op(OPERATOR_LSQUARE) || t->is_op(OPERATOR_LPAREN)
	      || t->is_op(OPERATOR_LCURLY))
	    ++depth;
	  else if (t->is_op(OPERATOR_RSQUARE) || t->is_op(OPERATOR_RPAREN)
		   || t->is_op(OPERATOR_RCURLY))
	    --depth;
	  arg.push_back(*t);
	  raw->push_back(*t);
	  this->advance_token();
	}
      groups->push_back(arg);
      if (this->peek_token()->is_op(OPERATOR_COMMA))
	{
	  raw->push_back(*this->peek_token());
	  this->advance_token();
	}
    }
  if (this->peek_token()->is_op(OPERATOR_RSQUARE))
    raw->push_back(*this->peek_token());
  // Consume "]".
  this->advance_token();
}

// Generics: parse a "[type-args]" list at a use site of a generic
// function and return a reference to the resulting instance.  The
// current token is "[".

Expression*
Parse::generic_instantiation(Generic_function_info* info, Expression* fn,
			     Location location)
{
  go_assert(this->peek_token()->is_op(OPERATOR_LSQUARE));
  this->advance_token();

  std::vector<std::vector<Token> > type_args;
  while (!this->peek_token()->is_op(OPERATOR_RSQUARE)
	 && !this->peek_token()->is_eof())
    {
      std::vector<Token> arg;
      int depth = 0;
      while (true)
	{
	  const Token* t = this->peek_token();
	  if (t->is_eof())
	    break;
	  if (depth == 0
	      && (t->is_op(OPERATOR_COMMA) || t->is_op(OPERATOR_RSQUARE)))
	    break;
	  if (t->is_op(OPERATOR_LSQUARE) || t->is_op(OPERATOR_LPAREN)
	      || t->is_op(OPERATOR_LCURLY))
	    ++depth;
	  else if (t->is_op(OPERATOR_RSQUARE) || t->is_op(OPERATOR_RPAREN)
		   || t->is_op(OPERATOR_RCURLY))
	    --depth;
	  arg.push_back(*t);
	  this->advance_token();
	}
      type_args.push_back(arg);
      if (this->peek_token()->is_op(OPERATOR_COMMA))
	this->advance_token();
    }
  // Consume "]".
  this->advance_token();

  // A partial type-argument list (fewer arguments than type parameters).
  // The remaining parameters may be determined by constraint type inference
  // alone (e.g. "f2[byte]" where the second parameter's constraint is
  // "interface{ []A }", giving []byte).
  if (type_args.size() < info->type_param_names().size() && fn != NULL)
    {
      // When a call follows ("F[int](args)"), the remaining parameters can be
      // inferred from the call arguments, which is more reliable; record the
      // explicit arguments and return the generic function reference unchanged
      // for the call expression to finish.  Completing here would also force a
      // premature resolve_global_names() mid-parse.  But when the partial
      // instantiation is used as a value ("f := f2[byte]") there is no call to
      // recover the arguments from, so try to complete it now from the
      // remaining parameters' own constraints.
      if (!this->peek_token()->is_op(OPERATOR_LPAREN))
	{
	  Expression_list no_args;
	  Named_object* ino =
	    this->instantiate_generic_with_inference(info, &no_args, location,
						     &type_args, false,
						     /*quiet=*/true);
	  if (ino != NULL)
	    return Expression::make_func_reference(ino, NULL, location);
	}
      partial_generic_type_args[fn] = type_args;
      // Capture the type arguments' package-qualifier bindings now, while the
      // file's imports are in scope, so a qualifier in an explicit argument
      // (e.g. "tpm2" in "tpm2.TPMTPublic") still resolves when the argument is
      // re-parsed during determine_types, after file-scope imports are cleared.
      {
	std::map<std::string, std::string> aliases;
	for (size_t i = 0; i < type_args.size(); ++i)
	  this->note_token_package_usage(type_args[i], &aliases);
	if (!aliases.empty())
	  partial_generic_type_arg_aliases[fn] = aliases;
      }
      return fn;
    }

  Named_object* ino = this->instantiate_generic_function(info, type_args,
							 location);
  if (ino == NULL)
    return Expression::make_error(location);
  return Expression::make_func_reference(ino, NULL, location);
}

// Selector = "." identifier .
// TypeGuard = "." "(" QualifiedIdent ")" .

// Note that Operand can expand to QualifiedIdent, which contains a
// ".".  That is handled directly in operand when it sees a package
// name.

// If IS_TYPE_SWITCH is not NULL, this will recognize a type switch
// guard (var := expr.("type") using the literal keyword "type").

Expression*
Parse::selector(Expression* left, bool* is_type_switch)
{
  go_assert(this->peek_token()->is_op(OPERATOR_DOT));
  Location location = this->location();

  const Token* token = this->advance_token();
  if (token->is_identifier())
    {
      // This could be a field in a struct, or a method in an
      // interface, or a method associated with a type.  We can't know
      // which until we have seen all the types.
      std::string name =
	this->gogo_->pack_hidden_name_for_field(token->identifier(),
						token->is_identifier_exported());
      if (token->identifier() == "_")
	{
	  go_error_at(this->location(), "invalid use of %<_%>");
	  name = Gogo::erroneous_name();
	}
      this->advance_token();
      return Expression::make_selector(left, name, location);
    }
  else if (token->is_op(OPERATOR_LPAREN))
    {
      this->advance_token();
      Type* type = NULL;
      if (!this->peek_token()->is_keyword(KEYWORD_TYPE))
	type = this->type();
      else
	{
	  if (is_type_switch != NULL)
	    *is_type_switch = true;
	  else
	    {
	      go_error_at(this->location(),
			  "use of %<.(type)%> outside type switch");
	      type = Type::make_error_type();
	    }
	  this->advance_token();
	}
      if (!this->peek_token()->is_op(OPERATOR_RPAREN))
	go_error_at(this->location(), "missing %<)%>");
      else
	this->advance_token();
      if (is_type_switch != NULL && *is_type_switch)
	return left;
      Expression* tg = Expression::make_type_guard(left, type, location);
      // In a generic instantiation, an assertion target derived from a type
      // parameter must not be statically rejected as impossible.
      if (this->replay_tokens_ != NULL
	  && tg->type_guard_expression() != NULL)
	tg->type_guard_expression()->set_is_instantiated();
      return tg;
    }
  else
    {
      go_error_at(this->location(), "expected identifier or %<(%>");
      return left;
    }
}

// Index          = "[" Expression "]" .
// Slice          = "[" Expression ":" [ Expression ] [ ":" Expression ] "]" .

Expression*
Parse::index(Expression* expr)
{
  Location location = this->location();
  go_assert(this->peek_token()->is_op(OPERATOR_LSQUARE));
  this->advance_token();

  Expression* start;
  if (!this->peek_token()->is_op(OPERATOR_COLON))
    start = this->expression(PRECEDENCE_NORMAL, false, true, NULL, NULL);
  else
    start = Expression::make_integer_ul(0, NULL, location);

  Expression* end = NULL;
  if (this->peek_token()->is_op(OPERATOR_COLON))
    {
      // We use nil to indicate a missing high expression.
      if (this->advance_token()->is_op(OPERATOR_RSQUARE))
	end = Expression::make_nil(this->location());
      else if (this->peek_token()->is_op(OPERATOR_COLON))
	{
	  go_error_at(this->location(),
		      "middle index required in 3-index slice");
	  end = Expression::make_error(this->location());
	}
      else
	end = this->expression(PRECEDENCE_NORMAL, false, true, NULL, NULL);
    }

  Expression* cap = NULL;
  if (this->peek_token()->is_op(OPERATOR_COLON))
    {
      if (this->advance_token()->is_op(OPERATOR_RSQUARE))
	{
	  go_error_at(this->location(),
		      "final index required in 3-index slice");
	  cap = Expression::make_error(this->location());
	}
      else
        cap = this->expression(PRECEDENCE_NORMAL, false, true, NULL, NULL);
    }
  if (!this->peek_token()->is_op(OPERATOR_RSQUARE))
    go_error_at(this->location(), "missing %<]%>");
  else
    this->advance_token();
  return Expression::make_index(expr, start, end, cap, location);
}

// Call           = "(" [ ArgumentList [ "," ] ] ")" .
// ArgumentList   = ExpressionList [ "..." ] .

Expression*
Parse::call(Expression* func)
{
  go_assert(this->peek_token()->is_op(OPERATOR_LPAREN));
  Expression_list* args = NULL;
  bool is_varargs = false;
  const Token* token = this->advance_token();
  if (!token->is_op(OPERATOR_RPAREN))
    {
      args = this->expression_list(NULL, false, true);
      token = this->peek_token();
      if (token->is_op(OPERATOR_ELLIPSIS))
	{
	  is_varargs = true;
	  token = this->advance_token();
	}
    }
  if (token->is_op(OPERATOR_COMMA))
    token = this->advance_token();
  if (!token->is_op(OPERATOR_RPAREN))
    {
      go_error_at(this->location(), "missing %<)%>");
      if (!this->skip_past_error(OPERATOR_RPAREN))
	return Expression::make_error(this->location());
    }
  this->advance_token();
  if (func->is_error_expression())
    return func;
  return Expression::make_call(func, args, is_varargs, func->location());
}

// Return an expression for a single unqualified identifier.

Expression*
Parse::id_to_expression(const std::string& name, Location location,
			bool is_lhs, bool is_composite_literal_key)
{
  Named_object* in_function;
  Named_object* named_object = this->gogo_->lookup(name, &in_function);
  if (named_object == NULL)
    {
      if (is_composite_literal_key)
	{
	  // This is a composite literal key, which means that it
	  // could just be a struct field name, so avoid confusion by
	  // not adding it to the bindings.  We'll look up the name
	  // later during the determine types phase if necessary.
	  return Expression::make_composite_literal_key(name, location);
	}
      named_object = this->gogo_->add_unknown_name(name, location);
    }

  if (in_function != NULL
      && in_function != this->gogo_->current_function()
      && (named_object->is_variable() || named_object->is_result_variable()))
    return this->enclosing_var_reference(in_function, named_object, is_lhs,
					 location);

  switch (named_object->classification())
    {
    case Named_object::NAMED_OBJECT_CONST:
      return Expression::make_const_reference(named_object, location);
    case Named_object::NAMED_OBJECT_VAR:
    case Named_object::NAMED_OBJECT_RESULT_VAR:
      if (!is_lhs)
	this->mark_var_used(named_object);
      return Expression::make_var_reference(named_object, location);
    case Named_object::NAMED_OBJECT_SINK:
      return Expression::make_sink(location);
    case Named_object::NAMED_OBJECT_FUNC:
    case Named_object::NAMED_OBJECT_FUNC_DECLARATION:
      return Expression::make_func_reference(named_object, NULL, location);
    case Named_object::NAMED_OBJECT_UNKNOWN:
      {
	Unknown_expression* ue =
	  Expression::make_unknown_reference(named_object, location);
	if (this->is_erroneous_function_)
	  ue->set_no_error_message();
	return ue;
      }
    case Named_object::NAMED_OBJECT_PACKAGE:
    case Named_object::NAMED_OBJECT_TYPE:
    case Named_object::NAMED_OBJECT_TYPE_DECLARATION:
      {
	// These cases can arise for a field name in a composite
	// literal.  Keep track of these as they might be fake uses of
	// the related package.
	Unknown_expression* ue =
	  Expression::make_unknown_reference(named_object, location);
	if (named_object->package() != NULL)
	  named_object->package()->note_fake_usage(ue);
	if (this->is_erroneous_function_)
	  ue->set_no_error_message();
	return ue;
      }
    case Named_object::NAMED_OBJECT_ERRONEOUS:
      return Expression::make_error(location);
    default:
      go_error_at(this->location(), "unexpected type of identifier");
      return Expression::make_error(location);
    }
}

// Expression = UnaryExpr { binary_op Expression } .

// PRECEDENCE is the precedence of the current operator.

// If MAY_BE_SINK is true, this expression may be "_".

// If MAY_BE_COMPOSITE_LIT is true, this expression may be a composite
// literal.

// If IS_TYPE_SWITCH is not NULL, this will recognize a type switch
// guard (var := expr.("type") using the literal keyword "type").

// If IS_PARENTHESIZED is not NULL, *IS_PARENTHESIZED is set to true
// if the entire expression is in parentheses.

Expression*
Parse::expression(Precedence precedence, bool may_be_sink,
		  bool may_be_composite_lit, bool* is_type_switch,
		  bool *is_parenthesized)
{
  Expression* left = this->unary_expr(may_be_sink, may_be_composite_lit,
				      is_type_switch, is_parenthesized);

  while (true)
    {
      if (is_type_switch != NULL && *is_type_switch)
	return left;

      const Token* token = this->peek_token();
      if (token->classification() != Token::TOKEN_OPERATOR)
	{
	  // Not a binary_op.
	  return left;
	}

      Precedence right_precedence;
      switch (token->op())
	{
	case OPERATOR_OROR:
	  right_precedence = PRECEDENCE_OROR;
	  break;
	case OPERATOR_ANDAND:
	  right_precedence = PRECEDENCE_ANDAND;
	  break;
	case OPERATOR_EQEQ:
	case OPERATOR_NOTEQ:
	case OPERATOR_LT:
	case OPERATOR_LE:
	case OPERATOR_GT:
	case OPERATOR_GE:
	  right_precedence = PRECEDENCE_RELOP;
	  break;
	case OPERATOR_PLUS:
	case OPERATOR_MINUS:
	case OPERATOR_OR:
	case OPERATOR_XOR:
	  right_precedence = PRECEDENCE_ADDOP;
	  break;
	case OPERATOR_MULT:
	case OPERATOR_DIV:
	case OPERATOR_MOD:
	case OPERATOR_LSHIFT:
	case OPERATOR_RSHIFT:
	case OPERATOR_AND:
	case OPERATOR_BITCLEAR:
	  right_precedence = PRECEDENCE_MULOP;
	  break;
	default:
	  right_precedence = PRECEDENCE_INVALID;
	  break;
	}

      if (right_precedence == PRECEDENCE_INVALID)
	{
	  // Not a binary_op.
	  return left;
	}

      if (is_parenthesized != NULL)
	*is_parenthesized = false;

      Operator op = token->op();
      Location binop_location = token->location();

      if (precedence >= right_precedence)
	{
	  // We've already seen A * B, and we see + C.  We want to
	  // return so that A * B becomes a group.
	  return left;
	}

      this->advance_token();

      left = this->verify_not_sink(left);
      Expression* right = this->expression(right_precedence, false,
					   may_be_composite_lit,
					   NULL, NULL);
      left = Expression::make_binary(op, left, right, binop_location);
    }
}

bool
Parse::expression_may_start_here()
{
  const Token* token = this->peek_token();
  switch (token->classification())
    {
    case Token::TOKEN_INVALID:
    case Token::TOKEN_EOF:
      return false;
    case Token::TOKEN_KEYWORD:
      switch (token->keyword())
	{
	case KEYWORD_CHAN:
	case KEYWORD_FUNC:
	case KEYWORD_MAP:
	case KEYWORD_STRUCT:
	case KEYWORD_INTERFACE:
	  return true;
	default:
	  return false;
	}
    case Token::TOKEN_IDENTIFIER:
      return true;
    case Token::TOKEN_STRING:
      return true;
    case Token::TOKEN_OPERATOR:
      switch (token->op())
	{
	case OPERATOR_PLUS:
	case OPERATOR_MINUS:
	case OPERATOR_NOT:
	case OPERATOR_XOR:
	case OPERATOR_MULT:
	case OPERATOR_CHANOP:
	case OPERATOR_AND:
	case OPERATOR_LPAREN:
	case OPERATOR_LSQUARE:
	  return true;
	default:
	  return false;
	}
    case Token::TOKEN_CHARACTER:
    case Token::TOKEN_INTEGER:
    case Token::TOKEN_FLOAT:
    case Token::TOKEN_IMAGINARY:
      return true;
    default:
      go_unreachable();
    }
}

// UnaryExpr = unary_op UnaryExpr | PrimaryExpr .

// If MAY_BE_SINK is true, this expression may be "_".

// If MAY_BE_COMPOSITE_LIT is true, this expression may be a composite
// literal.

// If IS_TYPE_SWITCH is not NULL, this will recognize a type switch
// guard (var := expr.("type") using the literal keyword "type").

// If IS_PARENTHESIZED is not NULL, *IS_PARENTHESIZED is set to true
// if the entire expression is in parentheses.

Expression*
Parse::unary_expr(bool may_be_sink, bool may_be_composite_lit,
		  bool* is_type_switch, bool* is_parenthesized)
{
  const Token* token = this->peek_token();

  // There is a complex parse for <- chan.  The choices are
  // Convert x to type <- chan int:
  //   (<- chan int)(x)
  // Receive from (x converted to type chan <- chan int):
  //   (<- chan <- chan int (x))
  // Convert x to type <- chan (<- chan int).
  //   (<- chan <- chan int)(x)
  if (token->is_op(OPERATOR_CHANOP))
    {
      Location location = token->location();
      if (this->advance_token()->is_keyword(KEYWORD_CHAN))
	{
	  Expression* expr = this->primary_expr(false, may_be_composite_lit,
						NULL, NULL);
	  if (expr->is_error_expression())
	    return expr;
	  else if (!expr->is_type_expression())
	    return Expression::make_receive(expr, location);
	  else
	    {
	      if (expr->type()->is_error_type())
		return expr;

	      // We picked up "chan TYPE", but it is not a type
	      // conversion.
	      Channel_type* ct = expr->type()->channel_type();
	      if (ct == NULL)
		{
		  // This is probably impossible.
		  go_error_at(location, "expected channel type");
		  return Expression::make_error(location);
		}
	      else if (ct->may_receive())
		{
		  // <- chan TYPE.
		  Type* t = Type::make_channel_type(false, true,
						    ct->element_type());
		  return Expression::make_type(t, location);
		}
	      else
		{
		  // <- chan <- TYPE.  Because we skipped the leading
		  // <-, we parsed this as chan <- TYPE.  With the
		  // leading <-, we parse it as <- chan (<- TYPE).
		  Type *t = this->reassociate_chan_direction(ct, location);
		  return Expression::make_type(t, location);
		}
	    }
	}

      this->unget_token(Token::make_operator_token(OPERATOR_CHANOP, location));
      token = this->peek_token();
    }

  if (token->is_op(OPERATOR_PLUS)
      || token->is_op(OPERATOR_MINUS)
      || token->is_op(OPERATOR_NOT)
      || token->is_op(OPERATOR_XOR)
      || token->is_op(OPERATOR_CHANOP)
      || token->is_op(OPERATOR_MULT)
      || token->is_op(OPERATOR_AND))
    {
      Location location = token->location();
      Operator op = token->op();
      this->advance_token();

      Expression* expr = this->unary_expr(false, may_be_composite_lit, NULL,
					  NULL);
      if (expr->is_error_expression())
	;
      else if (op == OPERATOR_MULT && expr->is_type_expression())
	expr = Expression::make_type(Type::make_pointer_type(expr->type()),
				     location);
      else if (op == OPERATOR_AND && expr->is_composite_literal())
	expr = Expression::make_heap_expression(expr, location);
      else if (op != OPERATOR_CHANOP)
	expr = Expression::make_unary(op, expr, location);
      else
	expr = Expression::make_receive(expr, location);
      return expr;
    }
  else
    return this->primary_expr(may_be_sink, may_be_composite_lit,
			      is_type_switch, is_parenthesized);
}

// This is called for the obscure case of
//   (<- chan <- chan int)(x)
// In unary_expr we remove the leading <- and parse the remainder,
// which gives us
//   chan <- (chan int)
// When we add the leading <- back in, we really want
//   <- chan (<- chan int)
// This means that we need to reassociate.

Type*
Parse::reassociate_chan_direction(Channel_type *ct, Location location)
{
  Channel_type* ele = ct->element_type()->channel_type();
  if (ele == NULL)
    {
      go_error_at(location, "parse error");
      return Type::make_error_type();
    }
  Type* sub = ele;
  if (ele->may_send())
    sub = Type::make_channel_type(false, true, ele->element_type());
  else
    sub = this->reassociate_chan_direction(ele, location);
  return Type::make_channel_type(false, true, sub);
}

// Statement =
//	Declaration | LabeledStmt | SimpleStmt |
//	GoStmt | ReturnStmt | BreakStmt | ContinueStmt | GotoStmt |
//	FallthroughStmt | Block | IfStmt | SwitchStmt | SelectStmt | ForStmt |
//	DeferStmt .

// LABEL is the label of this statement if it has one.

void
Parse::statement(Label* label)
{
  const Token* token = this->peek_token();
  switch (token->classification())
    {
    case Token::TOKEN_KEYWORD:
      {
	switch (token->keyword())
	  {
	  case KEYWORD_CONST:
	  case KEYWORD_TYPE:
	  case KEYWORD_VAR:
	    this->declaration();
	    break;
	  case KEYWORD_FUNC:
	  case KEYWORD_MAP:
	  case KEYWORD_STRUCT:
	  case KEYWORD_INTERFACE:
	    this->simple_stat(true, NULL, NULL, NULL);
	    break;
	  case KEYWORD_GO:
	  case KEYWORD_DEFER:
	    this->go_or_defer_stat();
	    break;
	  case KEYWORD_RETURN:
	    this->return_stat();
	    break;
	  case KEYWORD_BREAK:
	    this->break_stat();
	    break;
	  case KEYWORD_CONTINUE:
	    this->continue_stat();
	    break;
	  case KEYWORD_GOTO:
	    this->goto_stat();
	    break;
	  case KEYWORD_IF:
	    this->if_stat();
	    break;
	  case KEYWORD_SWITCH:
	    this->switch_stat(label);
	    break;
	  case KEYWORD_SELECT:
	    this->select_stat(label);
	    break;
	  case KEYWORD_FOR:
	    this->for_stat(label);
	    break;
	  default:
	    go_error_at(this->location(), "expected statement");
	    this->advance_token();
	    break;
	  }
      }
      break;

    case Token::TOKEN_IDENTIFIER:
      {
	std::string identifier = token->identifier();
	bool is_exported = token->is_identifier_exported();
	Location location = token->location();
	if (this->advance_token()->is_op(OPERATOR_COLON))
	  {
	    this->advance_token();
	    this->labeled_stmt(identifier, location);
	  }
	else
	  {
	    this->unget_token(Token::make_identifier_token(identifier,
							   is_exported,
							   location));
	    this->simple_stat(true, NULL, NULL, NULL);
	  }
      }
      break;

    case Token::TOKEN_OPERATOR:
      if (token->is_op(OPERATOR_LCURLY))
	{
	  Location location = token->location();
	  this->gogo_->start_block(location);
	  Location end_loc = this->block();
	  this->gogo_->add_block(this->gogo_->finish_block(end_loc),
				 location);
	}
      else if (!token->is_op(OPERATOR_SEMICOLON))
	this->simple_stat(true, NULL, NULL, NULL);
      break;

    case Token::TOKEN_STRING:
    case Token::TOKEN_CHARACTER:
    case Token::TOKEN_INTEGER:
    case Token::TOKEN_FLOAT:
    case Token::TOKEN_IMAGINARY:
      this->simple_stat(true, NULL, NULL, NULL);
      break;

    default:
      go_error_at(this->location(), "expected statement");
      this->advance_token();
      break;
    }
}

bool
Parse::statement_may_start_here()
{
  const Token* token = this->peek_token();
  switch (token->classification())
    {
    case Token::TOKEN_KEYWORD:
      {
	switch (token->keyword())
	  {
	  case KEYWORD_CONST:
	  case KEYWORD_TYPE:
	  case KEYWORD_VAR:
	  case KEYWORD_FUNC:
	  case KEYWORD_MAP:
	  case KEYWORD_STRUCT:
	  case KEYWORD_INTERFACE:
	  case KEYWORD_GO:
	  case KEYWORD_DEFER:
	  case KEYWORD_RETURN:
	  case KEYWORD_BREAK:
	  case KEYWORD_CONTINUE:
	  case KEYWORD_GOTO:
	  case KEYWORD_IF:
	  case KEYWORD_SWITCH:
	  case KEYWORD_SELECT:
	  case KEYWORD_FOR:
	    return true;

	  default:
	    return false;
	  }
      }
      break;

    case Token::TOKEN_IDENTIFIER:
      return true;

    case Token::TOKEN_OPERATOR:
      if (token->is_op(OPERATOR_LCURLY)
	  || token->is_op(OPERATOR_SEMICOLON))
	return true;
      else
	return this->expression_may_start_here();

    case Token::TOKEN_STRING:
    case Token::TOKEN_CHARACTER:
    case Token::TOKEN_INTEGER:
    case Token::TOKEN_FLOAT:
    case Token::TOKEN_IMAGINARY:
      return true;

    default:
      return false;
    }
}

// LabeledStmt = Label ":" Statement .
// Label       = identifier .

void
Parse::labeled_stmt(const std::string& label_name, Location location)
{
  Label* label = this->gogo_->add_label_definition(label_name, location);

  if (this->peek_token()->is_op(OPERATOR_RCURLY))
    {
      // This is a label at the end of a block.  A program is
      // permitted to omit a semicolon here.
      return;
    }

  if (!this->statement_may_start_here())
    {
      if (this->peek_token()->is_keyword(KEYWORD_FALLTHROUGH))
	{
	  // We don't treat the fallthrough keyword as a statement,
	  // because it can't appear most places where a statement is
	  // permitted, but it may have a label.  We introduce a
	  // semicolon because the caller expects to see a statement.
	  this->unget_token(Token::make_operator_token(OPERATOR_SEMICOLON,
						       location));
	  return;
	}

      // Mark the label as used to avoid a useless error about an
      // unused label.
      if (label != NULL)
        label->set_is_used();

      go_error_at(location, "missing statement after label");
      this->unget_token(Token::make_operator_token(OPERATOR_SEMICOLON,
						   location));
      return;
    }

  this->statement(label);
}

// SimpleStmt = EmptyStmt | ExpressionStmt | SendStmt | IncDecStmt |
//	Assignment | ShortVarDecl .

// EmptyStmt was handled in Parse::statement.

// In order to make this work for if and switch statements, if
// RETURN_EXP is not NULL, and we see an ExpressionStat, we return the
// expression rather than adding an expression statement to the
// current block.  If we see something other than an ExpressionStat,
// we add the statement, set *RETURN_EXP to true if we saw a send
// statement, and return NULL.  The handling of send statements is for
// better error messages.

// If P_RANGE_CLAUSE is not NULL, then this will recognize a
// RangeClause.

// If P_TYPE_SWITCH is not NULL, this will recognize a type switch
// guard (var := expr.("type") using the literal keyword "type").

Expression*
Parse::simple_stat(bool may_be_composite_lit, bool* return_exp,
		   Range_clause* p_range_clause, Type_switch* p_type_switch)
{
  const Token* token = this->peek_token();

  // An identifier follow by := is a SimpleVarDecl.
  if (token->is_identifier())
    {
      std::string identifier = token->identifier();
      bool is_exported = token->is_identifier_exported();
      Location location = token->location();

      token = this->advance_token();
      if (token->is_op(OPERATOR_COLONEQ)
	  || token->is_op(OPERATOR_COMMA))
	{
	  identifier = this->gogo_->pack_hidden_name(identifier, is_exported);
	  this->simple_var_decl_or_assignment(identifier, location,
					      may_be_composite_lit,
					      p_range_clause,
					      (token->is_op(OPERATOR_COLONEQ)
					       ? p_type_switch
					       : NULL));
	  return NULL;
	}

      this->unget_token(Token::make_identifier_token(identifier, is_exported,
						     location));
    }
  else if (p_range_clause != NULL && token->is_keyword(KEYWORD_RANGE))
    {
      Typed_identifier_list til;
      this->range_clause_decl(&til, p_range_clause);
      return NULL;
    }

  Expression* exp = this->expression(PRECEDENCE_NORMAL, true,
				     may_be_composite_lit,
				     (p_type_switch == NULL
				      ? NULL
				      : &p_type_switch->found),
				     NULL);
  if (p_type_switch != NULL && p_type_switch->found)
    {
      p_type_switch->name.clear();
      p_type_switch->location = exp->location();
      p_type_switch->expr = this->verify_not_sink(exp);
      return NULL;
    }
  token = this->peek_token();
  if (token->is_op(OPERATOR_CHANOP))
    {
      this->send_stmt(this->verify_not_sink(exp), may_be_composite_lit);
      if (return_exp != NULL)
	*return_exp = true;
    }
  else if (token->is_op(OPERATOR_PLUSPLUS)
	   || token->is_op(OPERATOR_MINUSMINUS))
    this->inc_dec_stat(this->verify_not_sink(exp));
  else if (token->is_op(OPERATOR_COMMA)
	   || token->is_op(OPERATOR_EQ))
    this->assignment(exp, may_be_composite_lit, p_range_clause);
  else if (token->is_op(OPERATOR_PLUSEQ)
	   || token->is_op(OPERATOR_MINUSEQ)
	   || token->is_op(OPERATOR_OREQ)
	   || token->is_op(OPERATOR_XOREQ)
	   || token->is_op(OPERATOR_MULTEQ)
	   || token->is_op(OPERATOR_DIVEQ)
	   || token->is_op(OPERATOR_MODEQ)
	   || token->is_op(OPERATOR_LSHIFTEQ)
	   || token->is_op(OPERATOR_RSHIFTEQ)
	   || token->is_op(OPERATOR_ANDEQ)
	   || token->is_op(OPERATOR_BITCLEAREQ))
    this->assignment(this->verify_not_sink(exp), may_be_composite_lit,
		     p_range_clause);
  else if (return_exp != NULL)
    return this->verify_not_sink(exp);
  else
    {
      exp = this->verify_not_sink(exp);

      if (token->is_op(OPERATOR_COLONEQ))
	{
	  if (!exp->is_error_expression())
	    go_error_at(token->location(), "non-name on left side of %<:=%>");
	  this->gogo_->mark_locals_used();
	  while (!token->is_op(OPERATOR_SEMICOLON)
		 && !token->is_eof())
	    token = this->advance_token();
	  return NULL;
	}

      this->expression_stat(exp);
    }

  return NULL;
}

bool
Parse::simple_stat_may_start_here()
{
  return this->expression_may_start_here();
}

// Parse { Statement ";" } which is used in a few places.  The list of
// statements may end with a right curly brace, in which case the
// semicolon may be omitted.

void
Parse::statement_list()
{
  while (this->statement_may_start_here())
    {
      this->statement(NULL);
      if (this->peek_token()->is_op(OPERATOR_SEMICOLON))
	this->advance_token();
      else if (this->peek_token()->is_op(OPERATOR_RCURLY))
	break;
      else
	{
	  if (!this->peek_token()->is_eof() || !saw_errors())
	    go_error_at(this->location(), "expected %<;%> or %<}%> or newline");
	  if (!this->skip_past_error(OPERATOR_RCURLY))
	    return;
	}
    }
}

bool
Parse::statement_list_may_start_here()
{
  return this->statement_may_start_here();
}

// ExpressionStat = Expression .

void
Parse::expression_stat(Expression* exp)
{
  this->gogo_->add_statement(Statement::make_statement(exp, false));
}

// SendStmt = Channel "&lt;-" Expression .
// Channel  = Expression .

void
Parse::send_stmt(Expression* channel, bool may_be_composite_lit)
{
  go_assert(this->peek_token()->is_op(OPERATOR_CHANOP));
  Location loc = this->location();
  this->advance_token();
  Expression* val = this->expression(PRECEDENCE_NORMAL, false,
				     may_be_composite_lit, NULL, NULL);
  Statement* s = Statement::make_send_statement(channel, val, loc);
  this->gogo_->add_statement(s);
}

// IncDecStat = Expression ( "++" | "--" ) .

void
Parse::inc_dec_stat(Expression* exp)
{
  const Token* token = this->peek_token();
  if (token->is_op(OPERATOR_PLUSPLUS))
    this->gogo_->add_statement(Statement::make_inc_statement(exp));
  else if (token->is_op(OPERATOR_MINUSMINUS))
    this->gogo_->add_statement(Statement::make_dec_statement(exp));
  else
    go_unreachable();
  this->advance_token();
}

// Assignment = ExpressionList assign_op ExpressionList .

// EXP is an expression that we have already parsed.

// If MAY_BE_COMPOSITE_LIT is true, an expression on the right hand
// side may be a composite literal.

// If RANGE_CLAUSE is not NULL, then this will recognize a
// RangeClause.

void
Parse::assignment(Expression* expr, bool may_be_composite_lit,
		  Range_clause* p_range_clause)
{
  Expression_list* vars;
  if (!this->peek_token()->is_op(OPERATOR_COMMA))
    {
      vars = new Expression_list();
      vars->push_back(expr);
    }
  else
    {
      this->advance_token();
      vars = this->expression_list(expr, true, may_be_composite_lit);
    }

  this->tuple_assignment(vars, may_be_composite_lit, p_range_clause);
}

// An assignment statement.  LHS is the list of expressions which
// appear on the left hand side.

// If MAY_BE_COMPOSITE_LIT is true, an expression on the right hand
// side may be a composite literal.

// If RANGE_CLAUSE is not NULL, then this will recognize a
// RangeClause.

void
Parse::tuple_assignment(Expression_list* lhs, bool may_be_composite_lit,
			Range_clause* p_range_clause)
{
  const Token* token = this->peek_token();
  if (!token->is_op(OPERATOR_EQ)
      && !token->is_op(OPERATOR_PLUSEQ)
      && !token->is_op(OPERATOR_MINUSEQ)
      && !token->is_op(OPERATOR_OREQ)
      && !token->is_op(OPERATOR_XOREQ)
      && !token->is_op(OPERATOR_MULTEQ)
      && !token->is_op(OPERATOR_DIVEQ)
      && !token->is_op(OPERATOR_MODEQ)
      && !token->is_op(OPERATOR_LSHIFTEQ)
      && !token->is_op(OPERATOR_RSHIFTEQ)
      && !token->is_op(OPERATOR_ANDEQ)
      && !token->is_op(OPERATOR_BITCLEAREQ))
    {
      go_error_at(this->location(), "expected assignment operator");
      return;
    }
  Operator op = token->op();
  Location location = token->location();

  token = this->advance_token();

  if (lhs == NULL)
    return;

  if (p_range_clause != NULL && token->is_keyword(KEYWORD_RANGE))
    {
      if (op != OPERATOR_EQ)
	go_error_at(this->location(), "range clause requires %<=%>");
      this->range_clause_expr(lhs, p_range_clause);
      return;
    }

  Expression_list* vals = this->expression_list(NULL, false,
						may_be_composite_lit);

  // We've parsed everything; check for errors.
  if (vals == NULL)
    return;
  for (Expression_list::const_iterator pe = lhs->begin();
       pe != lhs->end();
       ++pe)
    {
      if ((*pe)->is_error_expression())
	return;
      if (op != OPERATOR_EQ && (*pe)->is_sink_expression())
	go_error_at((*pe)->location(), "cannot use %<_%> as value");
    }
  for (Expression_list::const_iterator pe = vals->begin();
       pe != vals->end();
       ++pe)
    {
      if ((*pe)->is_error_expression())
	return;
    }

  Call_expression* call;
  Index_expression* map_index;
  Receive_expression* receive;
  Type_guard_expression* type_guard;
  if (lhs->size() == vals->size())
    {
      Statement* s;
      if (lhs->size() > 1)
	{
	  if (op != OPERATOR_EQ)
	    go_error_at(location, "multiple values only permitted with %<=%>");
	  s = Statement::make_tuple_assignment(lhs, vals, location);
	}
      else
	{
	  if (op == OPERATOR_EQ)
	    s = Statement::make_assignment(lhs->front(), vals->front(),
					   location);
	  else
	    s = Statement::make_assignment_operation(op, lhs->front(),
						     vals->front(), location);
	  delete lhs;
	  delete vals;
	}
      this->gogo_->add_statement(s);
    }
  else if (vals->size() == 1
	   && (call = (*vals->begin())->call_expression()) != NULL)
    {
      if (op != OPERATOR_EQ)
	go_error_at(location, "multiple results only permitted with %<=%>");
      call->set_expected_result_count(lhs->size());
      delete vals;
      vals = new Expression_list;
      for (unsigned int i = 0; i < lhs->size(); ++i)
	vals->push_back(Expression::make_call_result(call, i));
      Statement* s = Statement::make_tuple_assignment(lhs, vals, location);
      this->gogo_->add_statement(s);
    }
  else if (lhs->size() == 2
	   && vals->size() == 1
	   && (map_index = (*vals->begin())->index_expression()) != NULL)
    {
      if (op != OPERATOR_EQ)
	go_error_at(location, "two values from map requires %<=%>");
      Expression* val = lhs->front();
      Expression* present = lhs->back();
      Statement* s = Statement::make_tuple_map_assignment(val, present,
							  map_index, location);
      this->gogo_->add_statement(s);
    }
  else if (lhs->size() == 2
	   && vals->size() == 1
	   && (receive = (*vals->begin())->receive_expression()) != NULL)
    {
      if (op != OPERATOR_EQ)
	go_error_at(location, "two values from receive requires %<=%>");
      Expression* val = lhs->front();
      Expression* success = lhs->back();
      Expression* channel = receive->channel();
      Statement* s = Statement::make_tuple_receive_assignment(val, success,
							      channel,
							      location);
      this->gogo_->add_statement(s);
    }
  else if (lhs->size() == 2
	   && vals->size() == 1
	   && (type_guard = (*vals->begin())->type_guard_expression()) != NULL)
    {
      if (op != OPERATOR_EQ)
	go_error_at(location, "two values from type guard requires %<=%>");
      Expression* val = lhs->front();
      Expression* ok = lhs->back();
      Expression* expr = type_guard->expr();
      Type* type = type_guard->type();
      Statement* s = Statement::make_tuple_type_guard_assignment(val, ok,
								 expr, type,
								 location);
      this->gogo_->add_statement(s);
    }
  else
    {
      go_error_at(location, ("number of variables does not "
                             "match number of values"));
    }
}

// GoStat = "go" Expression .
// DeferStat = "defer" Expression .

void
Parse::go_or_defer_stat()
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_GO)
	     || this->peek_token()->is_keyword(KEYWORD_DEFER));
  bool is_go = this->peek_token()->is_keyword(KEYWORD_GO);
  Location stat_location = this->location();

  this->advance_token();
  Location expr_location = this->location();

  bool is_parenthesized = false;
  Expression* expr = this->expression(PRECEDENCE_NORMAL, false, true, NULL,
				      &is_parenthesized);
  Call_expression* call_expr = expr->call_expression();
  if (is_parenthesized || call_expr == NULL)
    {
      go_error_at(expr_location, "argument to go/defer must be function call");
      return;
    }

  // Make it easier to simplify go/defer statements by putting every
  // statement in its own block.
  this->gogo_->start_block(stat_location);
  Statement* stat;
  if (is_go)
    {
      stat = Statement::make_go_statement(call_expr, stat_location);
      call_expr->set_is_concurrent();
    }
  else
    {
      stat = Statement::make_defer_statement(call_expr, stat_location);
      call_expr->set_is_deferred();
    }
  this->gogo_->add_statement(stat);
  this->gogo_->add_block(this->gogo_->finish_block(stat_location),
			 stat_location);
}

// ReturnStat = "return" [ ExpressionList ] .

void
Parse::return_stat()
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_RETURN));
  Location location = this->location();
  this->advance_token();
  Expression_list* vals = NULL;
  if (this->expression_may_start_here())
    vals = this->expression_list(NULL, false, true);
  Named_object* function = this->gogo_->current_function();
  this->gogo_->add_statement(Statement::make_return_statement(function, vals,
							      location));

  if (vals == NULL && function->func_value()->results_are_named())
    {
      Function::Results* results = function->func_value()->result_variables();
      for (Function::Results::const_iterator p = results->begin();
	   p != results->end();
	   ++p)
	{
	  Named_object* no = this->gogo_->lookup((*p)->name(), NULL);
	  if (no == NULL)
	    go_assert(saw_errors());
	  else if (!no->is_result_variable())
	    go_error_at(location, "%qs is shadowed during return",
			(*p)->message_name().c_str());
	}
    }
}

// IfStmt = "if" [ SimpleStmt ";" ] Expression Block
//          [ "else" ( IfStmt | Block ) ] .

void
Parse::if_stat()
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_IF));
  Location location = this->location();
  this->advance_token();

  this->gogo_->start_block(location);

  bool saw_simple_stat = false;
  Expression* cond = NULL;
  bool saw_send_stmt = false;
  if (this->simple_stat_may_start_here())
    {
      cond = this->simple_stat(false, &saw_send_stmt, NULL, NULL);
      saw_simple_stat = true;
    }
  if (cond != NULL && this->peek_token()->is_op(OPERATOR_SEMICOLON))
    {
      // The SimpleStat is an expression statement.
      this->expression_stat(cond);
      cond = NULL;
    }
  if (cond == NULL)
    {
      if (this->peek_token()->is_op(OPERATOR_SEMICOLON))
	this->advance_token();
      else if (saw_simple_stat)
	{
	  if (saw_send_stmt)
	    go_error_at(this->location(),
			("send statement used as value; "
			 "use select for non-blocking send"));
	  else
	    go_error_at(this->location(),
			"expected %<;%> after statement in if expression");
	  if (!this->expression_may_start_here())
	    cond = Expression::make_error(this->location());
	}
      if (cond == NULL && this->peek_token()->is_op(OPERATOR_LCURLY))
	{
	  go_error_at(this->location(),
		      "missing condition in if statement");
	  cond = Expression::make_error(this->location());
	}
      if (cond == NULL)
	cond = this->expression(PRECEDENCE_NORMAL, false, false, NULL, NULL);
    }

  // Check for the easy error of a newline before starting the block.
  if (this->peek_token()->is_op(OPERATOR_SEMICOLON))
    {
      Location semi_loc = this->location();
      if (this->advance_token()->is_op(OPERATOR_LCURLY))
	go_error_at(semi_loc, "unexpected semicolon or newline, expecting %<{%> after if clause");
      // Otherwise we will get an error when we call this->block
      // below.
    }

  this->gogo_->start_block(this->location());
  Location end_loc = this->block();
  Block* then_block = this->gogo_->finish_block(end_loc);

  // Check for the easy error of a newline before "else".
  if (this->peek_token()->is_op(OPERATOR_SEMICOLON))
    {
      Location semi_loc = this->location();
      if (this->advance_token()->is_keyword(KEYWORD_ELSE))
	go_error_at(this->location(),
		 "unexpected semicolon or newline before %<else%>");
      else
	this->unget_token(Token::make_operator_token(OPERATOR_SEMICOLON,
						     semi_loc));
    }

  Block* else_block = NULL;
  if (this->peek_token()->is_keyword(KEYWORD_ELSE))
    {
      this->gogo_->start_block(this->location());
      const Token* token = this->advance_token();
      if (token->is_keyword(KEYWORD_IF))
	this->if_stat();
      else if (token->is_op(OPERATOR_LCURLY))
	this->block();
      else
	{
	  go_error_at(this->location(), "expected %<if%> or %<{%>");
	  this->statement(NULL);
	}
      else_block = this->gogo_->finish_block(this->location());
    }

  this->gogo_->add_statement(Statement::make_if_statement(cond, then_block,
							  else_block,
							  location));

  this->gogo_->add_block(this->gogo_->finish_block(this->location()),
			 location);
}

// SwitchStmt = ExprSwitchStmt | TypeSwitchStmt .
// ExprSwitchStmt = "switch" [ [ SimpleStat ] ";" ] [ Expression ]
//			"{" { ExprCaseClause } "}" .
// TypeSwitchStmt  = "switch" [ [ SimpleStat ] ";" ] TypeSwitchGuard
//			"{" { TypeCaseClause } "}" .
// TypeSwitchGuard = [ identifier ":=" ] Expression "." "(" "type" ")" .

void
Parse::switch_stat(Label* label)
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_SWITCH));
  Location location = this->location();
  this->advance_token();

  this->gogo_->start_block(location);

  bool saw_simple_stat = false;
  Expression* switch_val = NULL;
  bool saw_send_stmt = false;
  Type_switch type_switch;
  bool have_type_switch_block = false;
  if (this->simple_stat_may_start_here())
    {
      switch_val = this->simple_stat(false, &saw_send_stmt, NULL,
				     &type_switch);
      saw_simple_stat = true;
    }
  if (switch_val != NULL && this->peek_token()->is_op(OPERATOR_SEMICOLON))
    {
      // The SimpleStat is an expression statement.
      this->expression_stat(switch_val);
      switch_val = NULL;
    }
  if (switch_val == NULL && !type_switch.found)
    {
      if (this->peek_token()->is_op(OPERATOR_SEMICOLON))
	this->advance_token();
      else if (saw_simple_stat)
	{
	  if (saw_send_stmt)
	    go_error_at(this->location(),
			("send statement used as value; "
			 "use select for non-blocking send"));
	  else
	    go_error_at(this->location(),
			"expected %<;%> after statement in switch expression");
	}
      if (!this->peek_token()->is_op(OPERATOR_LCURLY))
	{
	  if (this->peek_token()->is_identifier())
	    {
	      const Token* token = this->peek_token();
	      std::string identifier = token->identifier();
	      bool is_exported = token->is_identifier_exported();
	      Location id_loc = token->location();

	      token = this->advance_token();
	      bool is_coloneq = token->is_op(OPERATOR_COLONEQ);
	      this->unget_token(Token::make_identifier_token(identifier,
							     is_exported,
							     id_loc));
	      if (is_coloneq)
		{
		  // This must be a TypeSwitchGuard.  It is in a
		  // different block from any initial SimpleStat.
		  if (saw_simple_stat)
		    {
		      this->gogo_->start_block(id_loc);
		      have_type_switch_block = true;
		    }

		  switch_val = this->simple_stat(false, &saw_send_stmt, NULL,
						 &type_switch);
		  if (!type_switch.found)
		    {
		      if (switch_val == NULL
			  || !switch_val->is_error_expression())
			{
			  go_error_at(id_loc,
				      "expected type switch assignment");
			  switch_val = Expression::make_error(id_loc);
			}
		    }
		}
	    }
	  if (switch_val == NULL && !type_switch.found)
	    {
	      switch_val = this->expression(PRECEDENCE_NORMAL, false, false,
					    &type_switch.found, NULL);
	      if (type_switch.found)
		{
		  type_switch.name.clear();
		  type_switch.expr = switch_val;
		  type_switch.location = switch_val->location();
		}
	    }
	}
    }

  if (!this->peek_token()->is_op(OPERATOR_LCURLY))
    {
      Location token_loc = this->location();
      if (this->peek_token()->is_op(OPERATOR_SEMICOLON)
	  && this->advance_token()->is_op(OPERATOR_LCURLY))
	go_error_at(token_loc, "missing %<{%> after switch clause");
      else if (this->peek_token()->is_op(OPERATOR_COLONEQ))
	{
	  go_error_at(token_loc, "invalid variable name");
	  this->advance_token();
	  this->expression(PRECEDENCE_NORMAL, false, false,
			   &type_switch.found, NULL);
	  if (this->peek_token()->is_op(OPERATOR_SEMICOLON))
	    this->advance_token();
	  if (!this->peek_token()->is_op(OPERATOR_LCURLY))
	    {
	      if (have_type_switch_block)
		this->gogo_->add_block(this->gogo_->finish_block(location),
				       location);
	      this->gogo_->add_block(this->gogo_->finish_block(location),
				     location);
	      return;
	    }
	  if (type_switch.found)
	    type_switch.expr = Expression::make_error(location);
	}
      else
	{
	  go_error_at(this->location(), "expected %<{%>");
	  if (have_type_switch_block)
	    this->gogo_->add_block(this->gogo_->finish_block(this->location()),
				   location);
	  this->gogo_->add_block(this->gogo_->finish_block(this->location()),
				 location);
	  return;
	}
    }
  this->advance_token();

  Statement* statement;
  if (type_switch.found)
    statement = this->type_switch_body(label, type_switch, location);
  else
    statement = this->expr_switch_body(label, switch_val, location);

  if (statement != NULL)
    this->gogo_->add_statement(statement);

  if (have_type_switch_block)
    this->gogo_->add_block(this->gogo_->finish_block(this->location()),
			   location);

  this->gogo_->add_block(this->gogo_->finish_block(this->location()),
			 location);
}

// The body of an expression switch.
//   "{" { ExprCaseClause } "}"

Statement*
Parse::expr_switch_body(Label* label, Expression* switch_val,
			Location location)
{
  Switch_statement* statement = Statement::make_switch_statement(switch_val,
								 location);

  this->push_break_statement(statement, label);

  Case_clauses* case_clauses = new Case_clauses();
  bool saw_default = false;
  while (!this->peek_token()->is_op(OPERATOR_RCURLY))
    {
      if (this->peek_token()->is_eof())
	{
	  if (!saw_errors())
	    go_error_at(this->location(), "missing %<}%>");
	  return NULL;
	}
      this->expr_case_clause(case_clauses, &saw_default);
    }
  this->advance_token();

  statement->add_clauses(case_clauses);

  this->pop_break_statement();

  return statement;
}

// ExprCaseClause = ExprSwitchCase ":" [ StatementList ] .
// FallthroughStat = "fallthrough" .

void
Parse::expr_case_clause(Case_clauses* clauses, bool* saw_default)
{
  Location location = this->location();

  bool is_default = false;
  Expression_list* vals = this->expr_switch_case(&is_default);

  if (!this->peek_token()->is_op(OPERATOR_COLON))
    {
      if (!saw_errors())
	go_error_at(this->location(), "expected %<:%>");
      return;
    }
  else
    this->advance_token();

  Block* statements = NULL;
  if (this->statement_list_may_start_here())
    {
      this->gogo_->start_block(this->location());
      this->statement_list();
      statements = this->gogo_->finish_block(this->location());
    }

  bool is_fallthrough = false;
  if (this->peek_token()->is_keyword(KEYWORD_FALLTHROUGH))
    {
      Location fallthrough_loc = this->location();
      is_fallthrough = true;
      while (this->advance_token()->is_op(OPERATOR_SEMICOLON))
	;
      if (this->peek_token()->is_op(OPERATOR_RCURLY))
	go_error_at(fallthrough_loc,
		    _("cannot fallthrough final case in switch"));
      else if (!this->peek_token()->is_keyword(KEYWORD_CASE)
	       && !this->peek_token()->is_keyword(KEYWORD_DEFAULT))
	{
	  go_error_at(fallthrough_loc, "fallthrough statement out of place");
	  while (!this->peek_token()->is_keyword(KEYWORD_CASE)
		 && !this->peek_token()->is_keyword(KEYWORD_DEFAULT)
		 && !this->peek_token()->is_op(OPERATOR_RCURLY)
		 && !this->peek_token()->is_eof())
	    {
	      if (this->statement_may_start_here())
		this->statement_list();
	      else
		this->advance_token();
	    }
	}
    }

  if (is_default)
    {
      if (*saw_default)
	{
	  go_error_at(location, "multiple defaults in switch");
	  return;
	}
      *saw_default = true;
    }

  if (is_default || vals != NULL)
    clauses->add(vals, is_default, statements, is_fallthrough, location);
}

// ExprSwitchCase = "case" ExpressionList | "default" .

Expression_list*
Parse::expr_switch_case(bool* is_default)
{
  const Token* token = this->peek_token();
  if (token->is_keyword(KEYWORD_CASE))
    {
      this->advance_token();
      return this->expression_list(NULL, false, true);
    }
  else if (token->is_keyword(KEYWORD_DEFAULT))
    {
      this->advance_token();
      *is_default = true;
      return NULL;
    }
  else
    {
      if (!saw_errors())
	go_error_at(this->location(), "expected %<case%> or %<default%>");
      if (!token->is_op(OPERATOR_RCURLY))
	this->advance_token();
      return NULL;
    }
}

// The body of a type switch.
//   "{" { TypeCaseClause } "}" .

Statement*
Parse::type_switch_body(Label* label, const Type_switch& type_switch,
			Location location)
{
  Expression* init = type_switch.expr;
  std::string var_name = type_switch.name;
  if (!var_name.empty())
    {
      if (Gogo::is_sink_name(var_name))
        {
	  go_error_at(type_switch.location,
		      "no new variables on left side of %<:=%>");
          var_name.clear();
        }
      else
	{
          Location loc = type_switch.location;
	  Temporary_statement* switch_temp =
              Statement::make_temporary(NULL, init, loc);
	  this->gogo_->add_statement(switch_temp);
          init = Expression::make_temporary_reference(switch_temp, loc);
	}
    }

  Type_switch_statement* statement =
      Statement::make_type_switch_statement(init, location);
  // In a generic instantiation (re-parsed from captured tokens), a case
  // type that is a type parameter may coincide with another case after
  // substitution; that is permitted, so suppress the duplicate-case check.
  if (this->replay_tokens_ != NULL)
    statement->set_is_instantiated();
  this->push_break_statement(statement, label);

  Type_case_clauses* case_clauses = new Type_case_clauses();
  bool saw_default = false;
  std::vector<Named_object*> implicit_vars;
  while (!this->peek_token()->is_op(OPERATOR_RCURLY))
    {
      if (this->peek_token()->is_eof())
	{
	  go_error_at(this->location(), "missing %<}%>");
	  return NULL;
	}
      this->type_case_clause(var_name, init, case_clauses, &saw_default,
                             &implicit_vars);
    }
  this->advance_token();

  statement->add_clauses(case_clauses);

  this->pop_break_statement();

  // If there is a type switch variable implicitly declared in each case clause,
  // check that it is used in at least one of the cases.
  if (!var_name.empty())
    {
      bool used = false;
      for (std::vector<Named_object*>::iterator p = implicit_vars.begin();
	   p != implicit_vars.end();
	   ++p)
	{
	  if ((*p)->var_value()->is_used())
	    {
	      used = true;
	      break;
	    }
	}
      if (!used)
	go_error_at(type_switch.location, "%qs declared but not used",
		    Gogo::message_name(var_name).c_str());
    }
  return statement;
}

// TypeCaseClause  = TypeSwitchCase ":" [ StatementList ] .
// IMPLICIT_VARS is the list of variables implicitly declared for each type
// case if there is a type switch variable declared.

void
Parse::type_case_clause(const std::string& var_name, Expression* init,
                        Type_case_clauses* clauses, bool* saw_default,
			std::vector<Named_object*>* implicit_vars)
{
  Location location = this->location();

  std::vector<Type*> types;
  bool is_default = false;
  this->type_switch_case(&types, &is_default);

  if (!this->peek_token()->is_op(OPERATOR_COLON))
    go_error_at(this->location(), "expected %<:%>");
  else
    this->advance_token();

  Block* statements = NULL;
  if (this->statement_list_may_start_here())
    {
      this->gogo_->start_block(this->location());
      if (!var_name.empty())
	{
	  Type* type = NULL;
          Location var_loc = init->location();
	  if (types.size() == 1)
	    {
	      type = types.front();
	      init = Expression::make_type_guard(init, type, location);
	      if (this->replay_tokens_ != NULL
		  && init->type_guard_expression() != NULL)
		init->type_guard_expression()->set_is_instantiated();
	    }

	  Variable* v = new Variable(type, init, false, false, false,
				     var_loc);
	  v->set_is_used();
	  v->set_is_type_switch_var();
	  implicit_vars->push_back(this->gogo_->add_variable(var_name, v));
	}
      this->statement_list();
      statements = this->gogo_->finish_block(this->location());
    }

  if (this->peek_token()->is_keyword(KEYWORD_FALLTHROUGH))
    {
      go_error_at(this->location(),
		  "fallthrough is not permitted in a type switch");
      if (this->advance_token()->is_op(OPERATOR_SEMICOLON))
	this->advance_token();
    }

  if (is_default)
    {
      go_assert(types.empty());
      if (*saw_default)
	{
	  go_error_at(location, "multiple defaults in type switch");
	  return;
	}
      *saw_default = true;
      clauses->add(NULL, false, true, statements, location);
    }
  else if (!types.empty())
    {
      for (std::vector<Type*>::const_iterator p = types.begin();
	   p + 1 != types.end();
	   ++p)
	clauses->add(*p, true, false, NULL, location);
      clauses->add(types.back(), false, false, statements, location);
    }
  else
    clauses->add(Type::make_error_type(), false, false, statements, location);
}

// TypeSwitchCase  = "case" type | "default"

// We accept a comma separated list of types.

void
Parse::type_switch_case(std::vector<Type*>* types, bool* is_default)
{
  const Token* token = this->peek_token();
  if (token->is_keyword(KEYWORD_CASE))
    {
      this->advance_token();
      while (true)
	{
	  Type* t = this->type();

	  if (!t->is_error_type())
	    types->push_back(t);
	  else
	    {
	      this->gogo_->mark_locals_used();
	      token = this->peek_token();
	      while (!token->is_op(OPERATOR_COLON)
		     && !token->is_op(OPERATOR_COMMA)
		     && !token->is_op(OPERATOR_RCURLY)
		     && !token->is_eof())
		token = this->advance_token();
	    }

	  if (!this->peek_token()->is_op(OPERATOR_COMMA))
	    break;
	  this->advance_token();
	}
    }
  else if (token->is_keyword(KEYWORD_DEFAULT))
    {
      this->advance_token();
      *is_default = true;
    }
  else
    {
      go_error_at(this->location(), "expected %<case%> or %<default%>");
      if (!token->is_op(OPERATOR_RCURLY))
	this->advance_token();
    }
}

// SelectStat = "select" "{" { CommClause } "}" .

void
Parse::select_stat(Label* label)
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_SELECT));
  Location location = this->location();
  const Token* token = this->advance_token();

  if (!token->is_op(OPERATOR_LCURLY))
    {
      Location token_loc = token->location();
      if (token->is_op(OPERATOR_SEMICOLON)
	  && this->advance_token()->is_op(OPERATOR_LCURLY))
	go_error_at(token_loc, "unexpected semicolon or newline before %<{%>");
      else
	{
	  go_error_at(this->location(), "expected %<{%>");
	  return;
	}
    }
  this->advance_token();

  Select_statement* statement = Statement::make_select_statement(location);

  this->push_break_statement(statement, label);

  Select_clauses* select_clauses = new Select_clauses();
  bool saw_default = false;
  while (!this->peek_token()->is_op(OPERATOR_RCURLY))
    {
      if (this->peek_token()->is_eof())
	{
	  go_error_at(this->location(), "expected %<}%>");
	  return;
	}
      this->comm_clause(select_clauses, &saw_default);
    }

  this->advance_token();

  statement->add_clauses(select_clauses);

  this->pop_break_statement();

  this->gogo_->add_statement(statement);
}

// CommClause = CommCase ":" { Statement ";" } .

void
Parse::comm_clause(Select_clauses* clauses, bool* saw_default)
{
  Location location = this->location();
  bool is_send = false;
  Expression* channel = NULL;
  Expression* val = NULL;
  Expression* closed = NULL;
  std::string varname;
  std::string closedname;
  bool is_default = false;
  bool got_case = this->comm_case(&is_send, &channel, &val, &closed,
				  &varname, &closedname, &is_default);

  if (this->peek_token()->is_op(OPERATOR_COLON))
    this->advance_token();
  else
    go_error_at(this->location(), "expected colon");

  this->gogo_->start_block(this->location());

  Named_object* var = NULL;
  if (!varname.empty())
    {
      // FIXME: LOCATION is slightly wrong here.
      Variable* v = new Variable(NULL, channel, false, false, false,
				 location);
      v->set_type_from_chan_element();
      var = this->gogo_->add_variable(varname, v);
    }

  Named_object* closedvar = NULL;
  if (!closedname.empty())
    {
      // FIXME: LOCATION is slightly wrong here.
      Variable* v = new Variable(Type::lookup_bool_type(), NULL,
				 false, false, false, location);
      closedvar = this->gogo_->add_variable(closedname, v);
    }

  this->statement_list();

  Block* statements = this->gogo_->finish_block(this->location());

  if (is_default)
    {
      if (*saw_default)
	{
	  go_error_at(location, "multiple defaults in select");
	  return;
	}
      *saw_default = true;
    }

  if (got_case)
    clauses->add(is_send, channel, val, closed, var, closedvar, is_default,
		 statements, location);
  else if (statements != NULL)
    {
      // Add the statements to make sure that any names they define
      // are traversed.
      this->gogo_->add_block(statements, location);
    }
}

// CommCase   = "case" ( SendStmt | RecvStmt ) | "default" .

bool
Parse::comm_case(bool* is_send, Expression** channel, Expression** val,
		 Expression** closed, std::string* varname,
		 std::string* closedname, bool* is_default)
{
  const Token* token = this->peek_token();
  if (token->is_keyword(KEYWORD_DEFAULT))
    {
      this->advance_token();
      *is_default = true;
    }
  else if (token->is_keyword(KEYWORD_CASE))
    {
      this->advance_token();
      if (!this->send_or_recv_stmt(is_send, channel, val, closed, varname,
				   closedname))
	return false;
    }
  else
    {
      go_error_at(this->location(), "expected %<case%> or %<default%>");
      if (!token->is_op(OPERATOR_RCURLY))
	this->advance_token();
      return false;
    }

  return true;
}

// RecvStmt   = [ Expression [ "," Expression ] ( "=" | ":=" ) ] RecvExpr .
// RecvExpr   = Expression .

bool
Parse::send_or_recv_stmt(bool* is_send, Expression** channel, Expression** val,
			 Expression** closed, std::string* varname,
			 std::string* closedname)
{
  const Token* token = this->peek_token();
  bool saw_comma = false;
  bool closed_is_id = false;
  if (token->is_identifier())
    {
      Gogo* gogo = this->gogo_;
      std::string recv_var = token->identifier();
      bool is_rv_exported = token->is_identifier_exported();
      Location recv_var_loc = token->location();
      token = this->advance_token();
      if (token->is_op(OPERATOR_COLONEQ))
	{
	  // case rv := <-c:
	  this->advance_token();
	  Expression* e = this->expression(PRECEDENCE_NORMAL, false, false,
					   NULL, NULL);
	  Receive_expression* re = e->receive_expression();
	  if (re == NULL)
	    {
	      if (!e->is_error_expression())
		go_error_at(this->location(), "expected receive expression");
	      return false;
	    }
	  if (recv_var == "_")
	    {
	      go_error_at(recv_var_loc,
			  "no new variables on left side of %<:=%>");
	      recv_var = Gogo::erroneous_name();
	    }
	  *is_send = false;
	  *varname = gogo->pack_hidden_name(recv_var, is_rv_exported);
	  *channel = re->channel();
	  return true;
	}
      else if (token->is_op(OPERATOR_COMMA))
	{
	  token = this->advance_token();
	  if (token->is_identifier())
	    {
	      std::string recv_closed = token->identifier();
	      bool is_rc_exported = token->is_identifier_exported();
	      Location recv_closed_loc = token->location();
	      closed_is_id = true;

	      token = this->advance_token();
	      if (token->is_op(OPERATOR_COLONEQ))
		{
		  // case rv, rc := <-c:
		  this->advance_token();
		  Expression* e = this->expression(PRECEDENCE_NORMAL, false,
						   false, NULL, NULL);
		  Receive_expression* re = e->receive_expression();
		  if (re == NULL)
		    {
		      if (!e->is_error_expression())
			go_error_at(this->location(),
				 "expected receive expression");
		      return false;
		    }
		  if (recv_var == "_" && recv_closed == "_")
		    {
		      go_error_at(recv_var_loc,
				  "no new variables on left side of %<:=%>");
		      recv_var = Gogo::erroneous_name();
		    }
		  *is_send = false;
		  if (recv_var != "_")
		    *varname = gogo->pack_hidden_name(recv_var,
						      is_rv_exported);
		  if (recv_closed != "_")
		    *closedname = gogo->pack_hidden_name(recv_closed,
							 is_rc_exported);
		  *channel = re->channel();
		  return true;
		}

	      this->unget_token(Token::make_identifier_token(recv_closed,
							     is_rc_exported,
							     recv_closed_loc));
	    }

	  *val = this->id_to_expression(gogo->pack_hidden_name(recv_var,
							       is_rv_exported),
					recv_var_loc, true, false);
	  saw_comma = true;
	}
      else
	this->unget_token(Token::make_identifier_token(recv_var,
						       is_rv_exported,
						       recv_var_loc));
    }

  // If SAW_COMMA is false, then we are looking at the start of the
  // send or receive expression.  If SAW_COMMA is true, then *VAL is
  // set and we just read a comma.

  Expression* e;
  if (saw_comma || !this->peek_token()->is_op(OPERATOR_CHANOP))
    {
      e = this->expression(PRECEDENCE_NORMAL, true, true, NULL, NULL);
      if (e->receive_expression() != NULL)
	{
	  *is_send = false;
	  *channel = e->receive_expression()->channel();
	  // This is 'case (<-c):'.  We now expect ':'.  If we see
	  // '<-', then we have case (<-c)<-v:
	  if (!this->peek_token()->is_op(OPERATOR_CHANOP))
	    return true;
	}
    }
  else
    {
      // case <-c:
      *is_send = false;
      this->advance_token();
      *channel = this->expression(PRECEDENCE_NORMAL, false, true, NULL, NULL);

      // The next token should be ':'.  If it is '<-', then we have
      // case <-c <- v:
      // which is to say, send on a channel received from a channel.
      if (!this->peek_token()->is_op(OPERATOR_CHANOP))
	return true;

      e = Expression::make_receive(*channel, (*channel)->location());
    }

  if (!saw_comma && this->peek_token()->is_op(OPERATOR_COMMA))
    {
      this->advance_token();
      // case v, e = <-c:
      if (!e->is_sink_expression())
	*val = e;
      e = this->expression(PRECEDENCE_NORMAL, true, true, NULL, NULL);
      saw_comma = true;
    }

  if (this->peek_token()->is_op(OPERATOR_EQ))
    {
      *is_send = false;
      this->advance_token();
      Location recvloc = this->location();
      Expression* recvexpr = this->expression(PRECEDENCE_NORMAL, false,
					      true, NULL, NULL);
      if (recvexpr->receive_expression() == NULL)
	{
	  go_error_at(recvloc, "missing %<<-%>");
	  return false;
	}
      *channel = recvexpr->receive_expression()->channel();
      if (saw_comma)
	{
	  // case v, e = <-c:
	  // *VAL is already set.
	  if (!e->is_sink_expression())
	    *closed = e;
	}
      else
	{
	  // case v = <-c:
	  if (!e->is_sink_expression())
	    *val = e;
	}
      return true;
    }

  if (saw_comma)
    {
      if (closed_is_id)
	go_error_at(this->location(), "expected %<=%> or %<:=%>");
      else
	go_error_at(this->location(), "expected %<=%>");
      return false;
    }

  if (this->peek_token()->is_op(OPERATOR_CHANOP))
    {
      // case c <- v:
      *is_send = true;
      *channel = this->verify_not_sink(e);
      this->advance_token();
      *val = this->expression(PRECEDENCE_NORMAL, false, true, NULL, NULL);
      return true;
    }

  go_error_at(this->location(), "expected %<<-%> or %<=%>");
  return false;
}

// ForStat = "for" [ Condition | ForClause | RangeClause ] Block .
// Condition = Expression .

void
Parse::for_stat(Label* label)
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_FOR));
  Location location = this->location();
  const Token* token = this->advance_token();

  // Open a block to hold any variables defined in the init statement
  // of the for statement.
  this->gogo_->start_block(location);

  Block* init = NULL;
  Expression* cond = NULL;
  Block* post = NULL;
  Range_clause range_clause;

  if (!token->is_op(OPERATOR_LCURLY))
    {
      if (token->is_keyword(KEYWORD_VAR))
	{
	  go_error_at(this->location(),
                      "var declaration not allowed in for initializer");
	  this->var_decl();
	}

      if (token->is_op(OPERATOR_SEMICOLON))
	this->for_clause(&cond, &post);
      else
	{
	  // We might be looking at a Condition, an InitStat, or a
	  // RangeClause.
	  bool saw_send_stmt = false;
	  cond = this->simple_stat(false, &saw_send_stmt, &range_clause, NULL);
	  if (!this->peek_token()->is_op(OPERATOR_SEMICOLON))
	    {
	      if (cond == NULL && !range_clause.found)
		{
		  if (saw_send_stmt)
		    go_error_at(this->location(),
                                ("send statement used as value; "
                                 "use select for non-blocking send"));
		  else
		    go_error_at(this->location(),
                                "parse error in for statement");
		}
	    }
	  else
	    {
	      if (range_clause.found)
		go_error_at(this->location(), "parse error after range clause");

	      if (cond != NULL)
		{
		  // COND is actually an expression statement for
		  // InitStat at the start of a ForClause.
		  this->expression_stat(cond);
		  cond = NULL;
		}

	      this->for_clause(&cond, &post);
	    }
	}
    }

  // Check for the easy error of a newline before starting the block.
  if (this->peek_token()->is_op(OPERATOR_SEMICOLON))
    {
      Location semi_loc = this->location();
      if (this->advance_token()->is_op(OPERATOR_LCURLY))
	go_error_at(semi_loc, "unexpected semicolon or newline, expecting %<{%> after for clause");
      // Otherwise we will get an error when we call this->block
      // below.
    }

  // Build the For_statement and note that it is the current target
  // for break and continue statements.

  For_statement* sfor;
  For_range_statement* srange;
  Statement* s;
  if (!range_clause.found)
    {
      sfor = Statement::make_for_statement(init, cond, post, location);
      s = sfor;
      srange = NULL;
    }
  else
    {
      srange = Statement::make_for_range_statement(range_clause.index,
						   range_clause.value,
						   range_clause.range,
						   location);
      s = srange;
      sfor = NULL;
    }

  this->push_break_statement(s, label);
  this->push_continue_statement(s, label);

  // Gather the block of statements in the loop and add them to the
  // For_statement.

  this->gogo_->start_block(this->location());
  Location end_loc = this->block();
  Block* statements = this->gogo_->finish_block(end_loc);

  if (sfor != NULL)
    sfor->add_statements(statements);
  else
    srange->add_statements(statements);

  // This is no longer the break/continue target.
  this->pop_break_statement();
  this->pop_continue_statement();

  // Add the For_statement to the list of statements, and close out
  // the block we started to hold any variables defined in the for
  // statement.

  this->gogo_->add_statement(s);

  this->gogo_->add_block(this->gogo_->finish_block(this->location()),
			 location);
}

// ForClause = [ InitStat ] ";" [ Condition ] ";" [ PostStat ] .
// InitStat = SimpleStat .
// PostStat = SimpleStat .

// We have already read InitStat at this point.

void
Parse::for_clause(Expression** cond, Block** post)
{
  go_assert(this->peek_token()->is_op(OPERATOR_SEMICOLON));
  this->advance_token();
  if (this->peek_token()->is_op(OPERATOR_SEMICOLON))
    *cond = NULL;
  else if (this->peek_token()->is_op(OPERATOR_LCURLY))
    {
      go_error_at(this->location(), "unexpected semicolon or newline, expecting %<{%> after for clause");
      *cond = NULL;
      *post = NULL;
      return;
    }
  else
    *cond = this->expression(PRECEDENCE_NORMAL, false, true, NULL, NULL);
  if (!this->peek_token()->is_op(OPERATOR_SEMICOLON))
    go_error_at(this->location(), "expected semicolon");
  else
    this->advance_token();

  if (this->peek_token()->is_op(OPERATOR_LCURLY))
    *post = NULL;
  else
    {
      this->gogo_->start_block(this->location());
      this->simple_stat(false, NULL, NULL, NULL);
      *post = this->gogo_->finish_block(this->location());
    }
}

// RangeClause = [ IdentifierList ( "=" | ":=" ) ] "range" Expression .

// This is the := version.  It is called with a list of identifiers.

void
Parse::range_clause_decl(const Typed_identifier_list* til,
			 Range_clause* p_range_clause)
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_RANGE));
  Location location = this->location();

  p_range_clause->found = true;

  if (til->size() > 2)
    go_error_at(this->location(), "too many variables for range clause");

  this->advance_token();
  Expression* expr = this->expression(PRECEDENCE_NORMAL, false, false, NULL,
				      NULL);
  p_range_clause->range = expr;

  if (til->empty())
    return;

  bool any_new = false;

  const Typed_identifier* pti = &til->front();
  Named_object* no = this->init_var(*pti, NULL, expr, true, true, &any_new,
				    NULL, NULL);
  if (any_new && no->is_variable())
    no->var_value()->set_type_from_range_index();
  p_range_clause->index = Expression::make_var_reference(no, location);

  if (til->size() == 1)
    p_range_clause->value = NULL;
  else
    {
      pti = &til->back();
      bool is_new = false;
      no = this->init_var(*pti, NULL, expr, true, true, &is_new, NULL, NULL);
      if (is_new && no->is_variable())
	no->var_value()->set_type_from_range_value();
      if (is_new)
	any_new = true;
      p_range_clause->value = Expression::make_var_reference(no, location);
    }

  if (!any_new)
    go_error_at(location, "variables redeclared but no variable is new");
}

// The = version of RangeClause.  This is called with a list of
// expressions.

void
Parse::range_clause_expr(const Expression_list* vals,
			 Range_clause* p_range_clause)
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_RANGE));

  p_range_clause->found = true;

  go_assert(vals->size() >= 1);
  if (vals->size() > 2)
    go_error_at(this->location(), "too many variables for range clause");

  this->advance_token();
  p_range_clause->range = this->expression(PRECEDENCE_NORMAL, false, false,
					   NULL, NULL);

  if (vals->empty())
    return;

  p_range_clause->index = vals->front();
  if (vals->size() == 1)
    p_range_clause->value = NULL;
  else
    p_range_clause->value = vals->back();
}

// Push a statement on the break stack.

void
Parse::push_break_statement(Statement* enclosing, Label* label)
{
  if (this->break_stack_ == NULL)
    this->break_stack_ = new Bc_stack();
  this->break_stack_->push_back(std::make_pair(enclosing, label));
}

// Push a statement on the continue stack.

void
Parse::push_continue_statement(Statement* enclosing, Label* label)
{
  if (this->continue_stack_ == NULL)
    this->continue_stack_ = new Bc_stack();
  this->continue_stack_->push_back(std::make_pair(enclosing, label));
}

// Pop the break stack.

void
Parse::pop_break_statement()
{
  this->break_stack_->pop_back();
}

// Pop the continue stack.

void
Parse::pop_continue_statement()
{
  this->continue_stack_->pop_back();
}

// Find a break or continue statement given a label name.

Statement*
Parse::find_bc_statement(const Bc_stack* bc_stack, const std::string& label)
{
  if (bc_stack == NULL)
    return NULL;
  for (Bc_stack::const_reverse_iterator p = bc_stack->rbegin();
       p != bc_stack->rend();
       ++p)
    {
      if (p->second != NULL && p->second->name() == label)
	{
	  p->second->set_is_used();
	  return p->first;
	}
    }
  return NULL;
}

// BreakStat = "break" [ identifier ] .

void
Parse::break_stat()
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_BREAK));
  Location location = this->location();

  const Token* token = this->advance_token();
  Statement* enclosing;
  if (!token->is_identifier())
    {
      if (this->break_stack_ == NULL || this->break_stack_->empty())
	{
	  go_error_at(this->location(),
                      "break statement not within for or switch or select");
	  return;
	}
      enclosing = this->break_stack_->back().first;
    }
  else
    {
      enclosing = this->find_bc_statement(this->break_stack_,
					  token->identifier());
      if (enclosing == NULL)
	{
	  // If there is a label with this name, mark it as used to
	  // avoid a useless error about an unused label.
	  this->gogo_->add_label_reference(token->identifier(),
                                           Linemap::unknown_location(), false);

	  go_error_at(token->location(), "invalid break label %qs",
                      Gogo::message_name(token->identifier()).c_str());
	  this->advance_token();
	  return;
	}
      this->advance_token();
    }

  Unnamed_label* label;
  if (enclosing->classification() == Statement::STATEMENT_FOR)
    label = enclosing->for_statement()->break_label();
  else if (enclosing->classification() == Statement::STATEMENT_FOR_RANGE)
    label = enclosing->for_range_statement()->break_label();
  else if (enclosing->classification() == Statement::STATEMENT_SWITCH)
    label = enclosing->switch_statement()->break_label();
  else if (enclosing->classification() == Statement::STATEMENT_TYPE_SWITCH)
    label = enclosing->type_switch_statement()->break_label();
  else if (enclosing->classification() == Statement::STATEMENT_SELECT)
    label = enclosing->select_statement()->break_label();
  else
    go_unreachable();

  this->gogo_->add_statement(Statement::make_break_statement(label,
							     location));
}

// ContinueStat = "continue" [ identifier ] .

void
Parse::continue_stat()
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_CONTINUE));
  Location location = this->location();

  const Token* token = this->advance_token();
  Statement* enclosing;
  if (!token->is_identifier())
    {
      if (this->continue_stack_ == NULL || this->continue_stack_->empty())
	{
	  go_error_at(this->location(), "continue statement not within for");
	  return;
	}
      enclosing = this->continue_stack_->back().first;
    }
  else
    {
      enclosing = this->find_bc_statement(this->continue_stack_,
					  token->identifier());
      if (enclosing == NULL)
	{
	  // If there is a label with this name, mark it as used to
	  // avoid a useless error about an unused label.
	  this->gogo_->add_label_reference(token->identifier(),
                                           Linemap::unknown_location(), false);

	  go_error_at(token->location(), "invalid continue label %qs",
                      Gogo::message_name(token->identifier()).c_str());
	  this->advance_token();
	  return;
	}
      this->advance_token();
    }

  Unnamed_label* label;
  if (enclosing->classification() == Statement::STATEMENT_FOR)
    label = enclosing->for_statement()->continue_label();
  else if (enclosing->classification() == Statement::STATEMENT_FOR_RANGE)
    label = enclosing->for_range_statement()->continue_label();
  else
    go_unreachable();

  this->gogo_->add_statement(Statement::make_continue_statement(label,
								location));
}

// GotoStat = "goto" identifier .

void
Parse::goto_stat()
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_GOTO));
  Location location = this->location();
  const Token* token = this->advance_token();
  if (!token->is_identifier())
    go_error_at(this->location(), "expected label for goto");
  else
    {
      Label* label = this->gogo_->add_label_reference(token->identifier(),
						      location, true);
      Statement* s = Statement::make_goto_statement(label, location);
      this->gogo_->add_statement(s);
      this->advance_token();
    }
}

// PackageClause = "package" PackageName .

void
Parse::package_clause()
{
  const Token* token = this->peek_token();
  Location location = token->location();
  std::string name;
  if (!token->is_keyword(KEYWORD_PACKAGE))
    {
      go_error_at(this->location(), "program must start with package clause");
      name = "ERROR";
    }
  else
    {
      token = this->advance_token();
      if (token->is_identifier())
	{
	  name = token->identifier();
	  if (name == "_")
	    {
	      go_error_at(this->location(), "invalid package name %<_%>");
	      name = Gogo::erroneous_name();
	    }
	  this->advance_token();
	}
      else
	{
	  go_error_at(this->location(), "package name must be an identifier");
	  name = "ERROR";
	}
    }
  this->gogo_->set_package_name(name, location);
}

// ImportDecl = "import" Decl<ImportSpec> .

void
Parse::import_decl()
{
  go_assert(this->peek_token()->is_keyword(KEYWORD_IMPORT));
  this->advance_token();
  this->decl(&Parse::import_spec);
}

// ImportSpec = [ "." | PackageName ] PackageFileName .

void
Parse::import_spec()
{
  this->check_directives();

  const Token* token = this->peek_token();
  Location location = token->location();

  std::string local_name;
  bool is_local_name_exported = false;
  if (token->is_op(OPERATOR_DOT))
    {
      local_name = ".";
      token = this->advance_token();
    }
  else if (token->is_identifier())
    {
      local_name = token->identifier();
      is_local_name_exported = token->is_identifier_exported();
      token = this->advance_token();
    }

  if (!token->is_string())
    {
      go_error_at(this->location(), "import path must be a string");
      this->advance_token();
      return;
    }

  this->gogo_->import_package(token->string_value(), local_name,
			      is_local_name_exported, true, location);

  this->advance_token();
}

// SourceFile       = PackageClause ";" { ImportDecl ";" }
//			{ TopLevelDecl ";" } .

void
Parse::program()
{
  this->package_clause();

  const Token* token = this->peek_token();
  if (token->is_op(OPERATOR_SEMICOLON))
    token = this->advance_token();
  else
    go_error_at(this->location(),
                "expected %<;%> or newline after package clause");

  while (token->is_keyword(KEYWORD_IMPORT))
    {
      this->import_decl();
      token = this->peek_token();
      if (token->is_op(OPERATOR_SEMICOLON))
	token = this->advance_token();
      else
	go_error_at(this->location(),
                    "expected %<;%> or newline after import declaration");
    }

  while (!token->is_eof())
    {
      if (this->declaration_may_start_here())
	this->declaration();
      else
	{
	  go_error_at(this->location(), "expected declaration");
	  this->gogo_->mark_locals_used();
	  do
	    this->advance_token();
	  while (!this->peek_token()->is_eof()
		 && !this->peek_token()->is_op(OPERATOR_SEMICOLON)
		 && !this->peek_token()->is_op(OPERATOR_RCURLY));
	  if (!this->peek_token()->is_eof()
	      && !this->peek_token()->is_op(OPERATOR_SEMICOLON))
	    this->advance_token();
	}
      token = this->peek_token();
      if (token->is_op(OPERATOR_SEMICOLON))
	token = this->advance_token();
      else if (!token->is_eof() || !saw_errors())
	{
	  if (token->is_op(OPERATOR_CHANOP))
	    go_error_at(this->location(),
                        ("send statement used as value; "
                         "use select for non-blocking send"));
	  else
	    go_error_at(this->location(),
                        ("expected %<;%> or newline after top "
                         "level declaration"));
	  this->skip_past_error(OPERATOR_INVALID);
	}
    }

  this->check_directives();
}

// If there are any pending compiler directives, clear them and give
// an error.  This is called when directives are not permitted.

void
Parse::check_directives()
{
  if (this->lex_->get_and_clear_pragmas() != 0)
    go_error_at(this->location(), "misplaced compiler directive");
  if (this->lex_->has_embeds())
    {
      this->lex_->clear_embeds();
      go_error_at(this->location(), "misplaced go:embed directive");
    }
}

// Skip forward to a semicolon or OP.  OP will normally be
// OPERATOR_RPAREN or OPERATOR_RCURLY.  If we find a semicolon, move
// past it and return.  If we find OP, it will be the next token to
// read.  Return true if we are OK, false if we found EOF.

bool
Parse::skip_past_error(Operator op)
{
  this->gogo_->mark_locals_used();
  const Token* token = this->peek_token();
  while (!token->is_op(op))
    {
      if (token->is_eof())
	return false;
      if (token->is_op(OPERATOR_SEMICOLON))
	{
	  this->advance_token();
	  return true;
	}
      token = this->advance_token();
    }
  return true;
}

// Check that an expression is not a sink.

Expression*
Parse::verify_not_sink(Expression* expr)
{
  if (expr->is_sink_expression())
    {
      go_error_at(expr->location(), "cannot use %<_%> as value");
      expr = Expression::make_error(expr->location());
    }

  // If this can not be a sink, and it is a variable, then we are
  // using the variable, not just assigning to it.
  if (expr->var_expression() != NULL)
    this->mark_var_used(expr->var_expression()->named_object());
  else if (expr->enclosed_var_expression() != NULL)
    this->mark_var_used(expr->enclosed_var_expression()->variable());
  return expr;
}

// Mark a variable as used.

void
Parse::mark_var_used(Named_object* no)
{
  if (no->is_variable())
    no->var_value()->set_is_used();
}
