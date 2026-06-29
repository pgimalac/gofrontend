// parse.h -- Go frontend parser.     -*- C++ -*-

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#ifndef GO_PARSE_H
#define GO_PARSE_H

class Lex;
class Gogo;
class Named_object;
class Type;
class Typed_identifier;
class Typed_identifier_list;
class Channel_type;
class Function_type;
class Block;
class Expression;
class Expression_list;
class Struct_field_list;
class Case_clauses;
class Type_case_clauses;
class Select_clauses;
class Statement;
class Label;
class Generic_function_info;
class Export;
class Import;
class Package;
class Bindings;

// Generics: export all exported generic function/type templates of the
// current package into the export stream.  Defined in parse.cc, where
// the Generic_function_info class is fully visible.
extern void
go_export_generics(Export*, Gogo*);

// Generics: read one generic template section from import data and
// register the template(s) in GOGO, associated with PACKAGE.
extern void
go_import_generics(Import*, Gogo*, Package*);

// Generics: add to EXPORTS any package-scope symbols (in particular
// unexported helpers) referenced by the bodies of exported generic
// templates, so that importing packages can resolve and link them when
// they instantiate those templates.
extern void
go_collect_generic_exports(Gogo*, const Bindings*,
			   Unordered_set(Named_object*)* exports);

// Parse the program.

class Parse
{
 public:
  Parse(Lex*, Gogo*);

  // Parse a program.
  void
  program();

  // Generics: infer the type arguments for a call to a generic function
  // from the argument expression types, then instantiate.  Returns the
  // instance Named_object, or NULL if inference fails.  Public because
  // it is invoked from Call_expression::do_determine_type.
  Named_object* instantiate_generic_with_inference(Generic_function_info*,
						   Expression_list* args,
						   Location,
						   const std::vector<std::vector<Token> >* partial = NULL,
						   bool call_is_spread = false,
						   bool quiet = false);

  // If EXPR is a use of a generic function with a partial type-argument
  // list ("F[int]" where F has more than one type parameter), return the
  // explicit type arguments; otherwise NULL.  Used by the call expression
  // to seed type inference.  Static because it consults file-scoped state.
  static const std::vector<std::vector<Token> >*
  partial_type_args_for(const Expression*);

  // Generics: resolve a deferred "F[args]" value (a forward reference used
  // with type arguments but no call) to either an instantiated generic
  // function reference or the fallback index expression, depending on what
  // the reference RESOLVED_NO turned out to be.  Returns NULL if the key was
  // not such a deferred value.  Static because it consults file-scoped state.
  static Expression*
  resolve_generic_value(Gogo*, const Expression* key, Named_object* resolved_no,
			Location);
  bool group_is_clearly_type(const std::vector<Token>& group);

  // Resolve recorded forward references to generic types.  Called from
  // go.cc after all input has been parsed.
  void resolve_pending_generic_types();

  // Check recorded generic instantiations against their type parameter
  // constraints.  Called from go.cc after determine_types.
  void check_generic_constraints();

 private:
  // Precedence values.
  enum Precedence
  {
    PRECEDENCE_INVALID = -1,
    PRECEDENCE_NORMAL = 0,
    PRECEDENCE_OROR,
    PRECEDENCE_ANDAND,
    PRECEDENCE_RELOP,
    PRECEDENCE_ADDOP,
    PRECEDENCE_MULOP
  };

  // We use this when parsing the range clause of a for statement.
  struct Range_clause
  {
    // Set to true if we found a range clause.
    bool found;
    // The index expression.
    Expression* index;
    // The value expression.
    Expression* value;
    // The range expression.
    Expression* range;

    Range_clause()
      : found(false), index(NULL), value(NULL), range(NULL)
    { }
  };

  // We use this when parsing the statement at the start of a switch,
  // in order to recognize type switches.
  struct Type_switch
  {
    // Set to true if we find a type switch.
    bool found;
    // The variable name.
    std::string name;
    // The location of the variable.
    Location location;
    // The expression.
    Expression* expr;

    Type_switch()
        : found(false), name(), location(Linemap::unknown_location()),
          expr(NULL)
    { }
  };

  // A variable defined in an enclosing function referenced by the
  // current function.
  class Enclosing_var
  {
   public:
    Enclosing_var(Named_object* var, Named_object* in_function,
		  unsigned int index)
      : var_(var), in_function_(in_function), index_(index)
    { }

    // We put these in a vector, so we need a default constructor.
    Enclosing_var()
      : var_(NULL), in_function_(NULL), index_(-1U)
    { }

    Named_object*
    var() const
    { return this->var_; }

    Named_object*
    in_function() const
    { return this->in_function_; }

    unsigned int
    index() const
    { return this->index_; }

   private:
    // The variable which is being referred to.
    Named_object* var_;
    // The function where the variable is defined.
    Named_object* in_function_;
    // The index of the field in this function's closure struct for
    // this variable.
    unsigned int index_;
  };

  // We store Enclosing_var entries in a set, so we need a comparator.
  struct Enclosing_var_comparison
  {
    bool
    operator()(const Enclosing_var&, const Enclosing_var&) const;
  };

  // A set of Enclosing_var entries.
  typedef std::set<Enclosing_var, Enclosing_var_comparison> Enclosing_vars;

  // Used to detect duplicate parameter/result names.
  typedef std::map<std::string, const Typed_identifier*> Names;

  // Fetch the next token from the lexer or, in replay mode, from the
  // captured token buffer.
  Token
  lex_next_token();

  // Peek at the current token from the lexer.
  const Token*
  peek_token();

  // Consume the current token, return the next one.
  const Token*
  advance_token();

  // Push a token back on the input stream.
  void
  unget_token(const Token&);

  // The location of the current token.
  Location
  location();

  // For break and continue we keep a stack of statements with
  // associated labels (if any).  The top of the stack is used for a
  // break or continue statement with no label.
  typedef std::vector<std::pair<Statement*, Label*> > Bc_stack;

  // Parser nonterminals.
  void identifier_list(Typed_identifier_list*);
  Expression_list* expression_list(Expression*, bool may_be_sink,
				   bool may_be_composite_lit);
  bool qualified_ident(std::string*, Named_object**);
  Type* type();
  bool type_may_start_here();
  Type* type_name(bool issue_error);
  Type* array_type(bool may_use_ellipsis);
  Type* map_type();
  Type* struct_type();
  void field_decl(Struct_field_list*);
  Type* pointer_type();
  Type* channel_type();
  void check_signature_names(const Typed_identifier_list*, Names*);
  Function_type* signature(Typed_identifier*, Location);
  bool parameters(Typed_identifier_list**, bool* is_varargs);
  Typed_identifier_list* parameter_list(bool* is_varargs);
  void parameter_decl(bool, Typed_identifier_list*, bool*, bool*, bool*);
  bool result(Typed_identifier_list**);
  Location block();
  Type* interface_type(bool record);
  void method_spec(Typed_identifier_list*);
  // Generics: parse and discard a constraint type element (used in
  // interface constraints), e.g. "~int" or "int | ~float64".
  void skip_constraint_term();
  // Capture the tokens of one constraint type-set element (an optional
  // "~" followed by a type), stopping at a top-level "|", ",", ";", "}"
  // or "]".
  void capture_constraint_element(std::vector<Token>* out);
  void declaration();
  bool declaration_may_start_here();
  void decl(void (Parse::*)());
  void list(void (Parse::*)(), bool);
  void const_decl();
  void const_spec(int, Type**, Expression_list**);
  void update_references(Expression**);
  void type_decl();
  void type_spec();
  void var_decl();
  void var_spec();
  void init_vars(const Typed_identifier_list*, Type*, Expression_list*,
		 bool is_coloneq, std::vector<std::string>*, Location);
  bool init_vars_from_call(const Typed_identifier_list*, Type*, Expression*,
			   bool is_coloneq, Location);
  bool init_vars_from_map(const Typed_identifier_list*, Type*, Expression*,
			  bool is_coloneq, Location);
  bool init_vars_from_receive(const Typed_identifier_list*, Type*,
			      Expression*, bool is_coloneq, Location);
  bool init_vars_from_type_guard(const Typed_identifier_list*, Type*,
				 Expression*, bool is_coloneq,
				 Location);
  Named_object* init_var(const Typed_identifier&, Type*, Expression*,
			 bool is_coloneq, bool type_from_init, bool* is_new,
			 Expression_list* vars, Expression_list* vals);
  Named_object* create_dummy_global(Type*, Expression*, Location);
  void finish_init_vars(Expression_list* vars, Expression_list* vals,
			Location);
  void simple_var_decl_or_assignment(const std::string&, Location,
				     bool may_be_composite_lit,
				     Range_clause*, Type_switch*);
  void function_decl();
  // Generics (gccgo extension).  Capture a generic function template
  // (one whose name is followed by a "[" type parameter list).
  void generic_function_decl(const std::string& name, bool is_exported,
			     Location, unsigned int pragmas);
  // Instantiate a generic function template with the given type
  // arguments (each given as a captured token sequence).  Returns the
  // Named_object for the (possibly cached) instance.
  Named_object* instantiate_generic_function(Generic_function_info*,
					     const std::vector<std::vector<Token> >&,
					     Location);
  // Switch this parser to read tokens from a captured token vector
  // instead of the lexer (used when re-parsing an instance).
  void set_replay_tokens(const std::vector<Token>* tokens);
  // Parse a "[type-args]" list at a use site of a generic function and
  // return a reference to the resulting instance.
  Expression* generic_instantiation(Generic_function_info*, Expression* fn,
				    Location);
  // Capture a bracketed list "[a, b, ...]" (current token is "[") into
  // comma-separated token groups, and also the raw "[...]" token sequence
  // (including the brackets).  Used to defer the index-versus-type-args
  // decision for a forward reference to a generic function.
  void capture_bracketed_type_args(std::vector<std::vector<Token> >* groups,
				   std::vector<Token>* raw);
  // Parse a "[name constraint, ...]" type parameter list (current token
  // is "[") collecting the parameter names and, if requested, the
  // constraint tokens of each parameter.
  void type_parameter_names(std::vector<std::string>* names,
			    std::vector<std::vector<Token> >* constraints = NULL);
  // Parse a captured token sequence as a type.
  Type* parse_type_from_tokens(const std::vector<Token>&);
  // Resolve a constraint type-set element's type without emitting errors,
  // resolving predeclared and package-global names through the global
  // bindings (which a throwaway re-parse cannot do for a name used only in
  // the constraint).  Returns NULL if it cannot be resolved.
  Type* resolve_constraint_type(const std::vector<Token>&);
  // For constraint type inference: if constraint C is a single structural
  // type element (e.g. "~map[K]V"), return that type parsed with the
  // generic's type-parameter NAMES replaced by inference markers, so it
  // can be unified against a solved type argument to solve the other
  // parameters.  Returns NULL if C is not a single structural element.
  Type* constraint_core_type_with_markers(const std::vector<Token>& c,
					  const std::vector<std::string>& names,
					  const std::vector<Type*>* solved = NULL);
  // Build a cache key for a generic instantiation from its type
  // arguments, canonicalized by resolved type identity where possible so
  // that different spellings of the same type map to one instance.
  std::string instance_key(const std::vector<std::vector<Token> >&);
  // Mark as used any imported package referenced (as "pkg.X") in a
  // captured generic template's tokens, so it is not reported as an
  // unused import even though the body is compiled only on instantiation.
  void note_token_package_usage(const std::vector<Token>&);
  // Capture a generic type template (a type whose name is followed by a
  // "[" type parameter list).
  void generic_type_decl(const std::string& name, bool is_exported, Location);
  // Whether the "[" at the current token (after a type name in a type
  // declaration) starts a type parameter list rather than an array or
  // slice element type.
  bool next_is_type_parameter_decl();
  // Record a balanced bracket group (the current token is the opener)
  // into *OUT, including both delimiters.
  void capture_bracket_group(std::vector<Token>* out);
  // Capture a method declaration with a generic receiver, e.g.
  // "func (s *Stack[T]) Push(x T) {...}", as a template on the generic
  // type.  RECV holds the already-captured receiver group tokens.
  void generic_method_decl(const std::vector<Token>& recv, Location,
			   unsigned int pragmas);
  // Re-parse a (token-substituted) method declaration as an ordinary
  // method on a generic type instance.  TOKS start at the receiver "(".
  Named_object* instantiate_generic_method(std::vector<Token>& toks, Location);
  // Instantiate a generic type template with the given type arguments
  // (each a captured token sequence).  Returns the instance type.
  Type* instantiate_generic_type(Generic_function_info*,
				 const std::vector<std::vector<Token> >&,
				 Location);
  // Parse a "[type-args]" list at a use site of a generic type, given
  // the template, and return the resulting instance type.
  Type* generic_type_instantiation(Generic_function_info*, Location);
  // A use of a generic type before its declaration: record a pending
  // instantiation and return a placeholder type, resolved later.  NAME
  // is the packed name of the referenced generic type.
  Type* pending_generic_type_instantiation(const std::string& name, Location);
  Type* make_pending_generic_type(const std::string& name,
				   const std::vector<std::vector<Token> >& type_args,
				   Location);
  std::string package_alias_for_local_type(Type* lt, Location);
  void localize_local_type_args(std::vector<std::vector<Token> >& type_args);
  Typed_identifier* receiver();
  Expression* operand(bool may_be_sink, bool *is_parenthesized);
  Expression* enclosing_var_reference(Named_object*, Named_object*,
				      bool may_be_sink, Location);
  Expression* composite_lit(Type*, int depth, Location);
  Expression* function_lit();
  Expression* create_closure(Named_object* function, Enclosing_vars*,
			     Location);
  Expression* primary_expr(bool may_be_sink, bool may_be_composite_lit,
			   bool* is_type_switch, bool* is_parenthesized);
  Expression* selector(Expression*, bool* is_type_switch);
  Expression* index(Expression*);
  Expression* call(Expression*);
  Expression* expression(Precedence, bool may_be_sink,
			 bool may_be_composite_lit, bool* is_type_switch,
			 bool *is_parenthesized);
  bool expression_may_start_here();
  Expression* unary_expr(bool may_be_sink, bool may_be_composite_lit,
			 bool* is_type_switch, bool* is_parenthesized);
  Type* reassociate_chan_direction(Channel_type*, Location);
  Expression* qualified_expr(Expression*, Location);
  Expression* id_to_expression(const std::string&, Location, bool, bool);
  void statement(Label*);
  bool statement_may_start_here();
  void labeled_stmt(const std::string&, Location);
  Expression* simple_stat(bool, bool*, Range_clause*, Type_switch*);
  bool simple_stat_may_start_here();
  void statement_list();
  bool statement_list_may_start_here();
  void expression_stat(Expression*);
  void send_stmt(Expression*, bool may_be_composite_lit);
  void inc_dec_stat(Expression*);
  void assignment(Expression*, bool may_be_composite_lit, Range_clause*);
  void tuple_assignment(Expression_list*, bool may_be_composite_lit,
			Range_clause*);
  void send();
  void go_or_defer_stat();
  void return_stat();
  void if_stat();
  void switch_stat(Label*);
  Statement* expr_switch_body(Label*, Expression*, Location);
  void expr_case_clause(Case_clauses*, bool* saw_default);
  Expression_list* expr_switch_case(bool*);
  Statement* type_switch_body(Label*, const Type_switch&, Location);
  void type_case_clause(const std::string&, Expression*, Type_case_clauses*,
                        bool* saw_default, std::vector<Named_object*>*);
  void type_switch_case(std::vector<Type*>*, bool*);
  void select_stat(Label*);
  void comm_clause(Select_clauses*, bool* saw_default);
  bool comm_case(bool*, Expression**, Expression**, Expression**,
		 std::string*, std::string*, bool*);
  bool send_or_recv_stmt(bool*, Expression**, Expression**, Expression**,
			 std::string*, std::string*);
  void for_stat(Label*);
  void for_clause(Expression**, Block**);
  void range_clause_decl(const Typed_identifier_list*, Range_clause*);
  void range_clause_expr(const Expression_list*, Range_clause*);
  void push_break_statement(Statement*, Label*);
  void push_continue_statement(Statement*, Label*);
  void pop_break_statement();
  void pop_continue_statement();
  Statement* find_bc_statement(const Bc_stack*, const std::string&);
  void break_stat();
  void continue_stat();
  void goto_stat();
  void package_clause();
  void import_decl();
  void import_spec();

  // Check for unused compiler directives.
  void check_directives();

  // Skip past an error looking for a semicolon or OP.  Return true if
  // all is well, false if we found EOF.
  bool
  skip_past_error(Operator op);

  // Verify that an expression is not a sink, and return either the
  // expression or an error.
  Expression*
  verify_not_sink(Expression*);

  // Return the statement associated with a label in a Bc_stack, or
  // NULL.
  Statement*
  find_bc_statement(const Bc_stack*, const std::string&) const;

  // Mark a variable as used.
  void
  mark_var_used(Named_object*);

  // The lexer output we are parsing.
  Lex* lex_;
  // When non-NULL, tokens are replayed from this vector (set up by
  // set_replay_tokens) instead of being read from the lexer.  Used
  // when re-parsing a generic function instance.
  const std::vector<Token>* replay_tokens_;
  // The index of the next token to return from replay_tokens_.
  size_t replay_index_;
  // The current token.
  Token token_;
  // Tokens pushed back on the input stream, as a stack: the next token
  // returned by peek_token is ungot_.back().  A stack (rather than a
  // single slot) allows a few tokens of lookahead-and-restore.
  std::vector<Token> ungot_;
  // Whether the function we are parsing had errors in the signature.
  bool is_erroneous_function_;
  // The code we are generating.
  Gogo* gogo_;
  // A stack of statements for which break may be used.
  Bc_stack* break_stack_;
  // A stack of statements for which continue may be used.
  Bc_stack* continue_stack_;
  // References from the local function to variables defined in
  // enclosing functions.
  Enclosing_vars enclosing_vars_;
  // Generics: while parsing an interface type, the constraint type-set
  // elements seen so far (each entry the tokens of one element, possibly
  // beginning with "~").  Method elements are not recorded here.
  std::vector<std::vector<Token> > iface_terms_;
  // The constraint type-set elements of the most recently parsed
  // interface type, for type_spec to associate with a named constraint.
  std::vector<std::vector<Token> > last_iface_terms_;
};


#endif // !defined(GO_PARSE_H)
